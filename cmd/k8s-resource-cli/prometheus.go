package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
)

func getPrometheusDeploymentMetrics(ctx context.Context, client *PrometheusClient, namespace, deploymentName, window string) ([]DeploymentMetrics, error) {
	nsFilter := ""
	if namespace != "" {
		nsFilter = fmt.Sprintf(`namespace="%s",`, namespace)
	}

	// Discover deployments from kube-state-metrics
	discoverQuery := fmt.Sprintf(`group by (namespace, deployment) (kube_deployment_info{%s})`, nsFilter)
	discovered, err := prometheusQueryVector(client, discoverQuery)
	if err != nil {
		return nil, fmt.Errorf("discovering deployments: %w", err)
	}

	if len(discovered) == 0 {
		return nil, nil
	}

	// Collect per-deployment metrics in bulk then index by "namespace/name"
	cpuAvgMap, err := prometheusQueryDeploymentMap(client, buildCPUAvgQuery(nsFilter, window))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: error querying CPU average: %v\n", err)
	}
	cpuMaxMap, err := prometheusQueryDeploymentMap(client, buildCPUMaxQuery(nsFilter, window))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: error querying CPU max: %v\n", err)
	}
	memAvgMap, err := prometheusQueryDeploymentMap(client, buildMemAvgQuery(nsFilter, window))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: error querying memory average: %v\n", err)
	}
	memMaxMap, err := prometheusQueryDeploymentMap(client, buildMemMaxQuery(nsFilter, window))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: error querying memory max: %v\n", err)
	}
	cpuReqMap, err := prometheusQueryDeploymentMap(client, buildCPURequestsQuery(nsFilter))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: error querying CPU requests: %v\n", err)
	}
	memReqMap, err := prometheusQueryDeploymentMap(client, buildMemRequestsQuery(nsFilter))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: error querying memory requests: %v\n", err)
	}
	replicasMap, err := prometheusQueryDeploymentMap(client, buildReplicasQuery(nsFilter))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: error querying replicas: %v\n", err)
	}
	hpaMaxMap, err := prometheusQueryDeploymentMap(client, buildHPAMaxQuery(nsFilter))
	if err != nil {
		// HPA metrics are optional
		if client.Debug {
			fmt.Fprintf(os.Stderr, "Debug: HPA metrics not available: %v\n", err)
		}
	}

	var results []DeploymentMetrics
	for _, r := range discovered {
		ns := r.Metric["namespace"]
		name := r.Metric["deployment"]
		if name == "" {
			continue
		}
		if deploymentName != "" && name != deploymentName {
			continue
		}

		key := ns + "/" + name

		replicas := int32(replicasMap[key])
		maxReplicas := replicas
		if v, ok := hpaMaxMap[key]; ok {
			if int32(v) > maxReplicas {
				maxReplicas = int32(v)
			}
		}

		// CPU in cores from Prometheus → convert to millicores
		cpuAvgMillis := int64(cpuAvgMap[key] * 1000)
		cpuMaxMillis := int64(cpuMaxMap[key] * 1000)
		cpuReqMillis := int64(cpuReqMap[key] * 1000)

		// Memory in bytes from Prometheus
		memAvgBytes := int64(memAvgMap[key])
		memMaxBytes := int64(memMaxMap[key])
		memReqBytes := int64(memReqMap[key])

		maxReqCPU := cpuReqMillis
		maxReqMem := memReqBytes
		if maxReplicas > replicas && replicas > 0 {
			maxReqCPU = cpuReqMillis * int64(maxReplicas) / int64(replicas)
			maxReqMem = memReqBytes * int64(maxReplicas) / int64(replicas)
		}

		results = append(results, DeploymentMetrics{
			Name:            name,
			Namespace:       ns,
			Type:            "Deployment",
			CurrentReplicas: replicas,
			DesiredReplicas: replicas,
			MaxReplicas:     maxReplicas,
			Requests:        ResourceMetrics{CPU: cpuReqMillis, Memory: memReqBytes},
			MaxRequests:     ResourceMetrics{CPU: maxReqCPU, Memory: maxReqMem},
			AvgUsage:        ResourceMetrics{CPU: cpuAvgMillis, Memory: memAvgBytes},
			MaxUsage:        ResourceMetrics{CPU: cpuMaxMillis, Memory: memMaxBytes},
		})
	}

	return results, nil
}

// prometheusQueryVector runs an instant query and returns all result series.
func prometheusQueryVector(client *PrometheusClient, query string) ([]PrometheusResult, error) {
	endpoint := client.BaseURL + "/api/v1/query"
	params := url.Values{"query": {query}}
	req, err := http.NewRequest("GET", endpoint+"?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}

	if client.Debug {
		fmt.Fprintf(os.Stderr, "Debug: Prometheus query: %s\n", query)
	}

	resp, err := client.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("prometheus returned HTTP %d: %s", resp.StatusCode, body)
	}

	var pr PrometheusResponse
	if err := json.Unmarshal(body, &pr); err != nil {
		return nil, fmt.Errorf("parsing prometheus response: %w", err)
	}
	if pr.Status != "success" {
		return nil, fmt.Errorf("prometheus query failed: status=%s", pr.Status)
	}

	return pr.Data.Result, nil
}

// prometheusQueryDeploymentMap runs an instant query expecting results labelled with
// "namespace" and "deployment", returning a map keyed by "namespace/deployment".
func prometheusQueryDeploymentMap(client *PrometheusClient, query string) (map[string]float64, error) {
	results, err := prometheusQueryVector(client, query)
	if err != nil {
		return nil, err
	}

	m := make(map[string]float64, len(results))
	for _, r := range results {
		ns := r.Metric["namespace"]
		dep := r.Metric["deployment"]
		if dep == "" {
			continue
		}
		key := ns + "/" + dep
		valStr, ok := r.Value[1].(string)
		if !ok {
			continue
		}
		val, err := strconv.ParseFloat(valStr, 64)
		if err != nil {
			continue
		}
		m[key] = val
	}
	return m, nil
}

// label_replace expression that strips the two trailing hash segments from a pod name
// to recover the deployment name, e.g. "api-server-6d4f9b-xk2pq" → "api-server".
const podToDeploymentReplace = `"deployment","$1","pod","^(.+)-[a-z0-9]+-[a-z0-9]+$"`

func buildCPUAvgQuery(nsFilter, window string) string {
	return fmt.Sprintf(
		`avg_over_time(sum by (namespace, deployment) (label_replace(rate(container_cpu_usage_seconds_total{%scontainer!="",container!="POD"}[5m]),%s))[%s:5m])`,
		nsFilter, podToDeploymentReplace, window,
	)
}

func buildCPUMaxQuery(nsFilter, window string) string {
	return fmt.Sprintf(
		`max_over_time(sum by (namespace, deployment) (label_replace(rate(container_cpu_usage_seconds_total{%scontainer!="",container!="POD"}[5m]),%s))[%s:5m])`,
		nsFilter, podToDeploymentReplace, window,
	)
}

func buildMemAvgQuery(nsFilter, window string) string {
	return fmt.Sprintf(
		`avg_over_time(sum by (namespace, deployment) (label_replace(container_memory_working_set_bytes{%scontainer!="",container!="POD"},%s))[%s:5m])`,
		nsFilter, podToDeploymentReplace, window,
	)
}

func buildMemMaxQuery(nsFilter, window string) string {
	return fmt.Sprintf(
		`max_over_time(sum by (namespace, deployment) (label_replace(container_memory_working_set_bytes{%scontainer!="",container!="POD"},%s))[%s:5m])`,
		nsFilter, podToDeploymentReplace, window,
	)
}

func buildCPURequestsQuery(nsFilter string) string {
	return fmt.Sprintf(
		`sum by (namespace, deployment) (label_replace(kube_pod_container_resource_requests{%sresource="cpu",container!=""},%s))`,
		nsFilter, podToDeploymentReplace,
	)
}

func buildMemRequestsQuery(nsFilter string) string {
	return fmt.Sprintf(
		`sum by (namespace, deployment) (label_replace(kube_pod_container_resource_requests{%sresource="memory",container!=""},%s))`,
		nsFilter, podToDeploymentReplace,
	)
}

func buildReplicasQuery(nsFilter string) string {
	return fmt.Sprintf(
		`kube_deployment_spec_replicas{%s}`,
		nsFilter,
	)
}

func buildHPAMaxQuery(nsFilter string) string {
	// Rename scaletargetref_name → deployment so the result is keyed the same way as other queries.
	// Only include HPAs that target Deployments.
	return fmt.Sprintf(
		`label_replace(kube_horizontalpodautoscaler_spec_max_replicas{%sscaletargetref_kind="Deployment"},"deployment","$1","scaletargetref_name","(.+)")`,
		nsFilter,
	)
}
