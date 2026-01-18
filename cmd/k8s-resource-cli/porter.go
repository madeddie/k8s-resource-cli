package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

func getPorterApplicationMetrics(ctx context.Context, client *PorterClient, appName string) ([]DeploymentMetrics, error) {
	// List all applications
	apps, err := client.ListApplications(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list applications: %w", err)
	}

	var deployments []DeploymentMetrics

	totalApps := len(apps)
	spinnerChars := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

	for i, app := range apps {
		// Skip if filtering by name
		if appName != "" && app.Name != appName {
			continue
		}

		// Show progress indicator with spinner
		spinner := spinnerChars[i%len(spinnerChars)]
		showProgress(os.Stderr, spinner, i+1, totalApps, app.Name)

		// Get application details
		detail, err := client.GetApplication(ctx, app.ID)
		if err != nil {
			// Clear spinner line before showing warning
			clearProgress(os.Stderr)
			fmt.Fprintf(os.Stderr, "Warning: Error getting application %s: %v\n", app.Name, err)
			continue
		}

		// Get deployment target info for cluster name
		clusterName := detail.DeploymentTargetID // fallback to ID
		if target, err := client.GetDeploymentTarget(ctx, detail.DeploymentTargetID); err == nil {
			if target.Name != "" {
				clusterName = target.Name

				// Check if we need to prefix with cluster name
				if target.ClusterID != 0 {
					if cluster, err := client.GetCluster(ctx, target.ClusterID); err == nil && cluster.Name != "" {
						// Check if cluster name is already in the deployment target name
						if !strings.HasPrefix(clusterName, cluster.Name) {
							clusterName = cluster.Name + "-" + clusterName
						}
					}
				}
			}
		} else if client.Debug {
			// Clear spinner line before showing debug message
			clearProgress(os.Stderr)
			fmt.Fprintf(os.Stderr, "DEBUG - Error getting deployment target %s: %v\n", detail.DeploymentTargetID, err)
		}

		// Process each service in the application
		for _, service := range detail.Services {
			// Determine min and max replicas
			minReplicas := service.Instances
			maxReplicas := service.Instances
			if service.Autoscaling != nil && service.Autoscaling.Enabled {
				minReplicas = service.Autoscaling.MinInstances
				maxReplicas = service.Autoscaling.MaxInstances
			}

			dm := DeploymentMetrics{
				Name:            fmt.Sprintf("%s-%s", app.Name, service.Name),
				Namespace:       clusterName,
				Type:            "Deployment",
				CurrentReplicas: service.Instances,
				DesiredReplicas: minReplicas,
				MaxReplicas:     maxReplicas,
			}

			// Convert CPU cores to millicores and memory MB to bytes
			cpuMillis := int64(service.CPUCores * 1000)
			memoryBytes := service.RAMMegabytes * 1024 * 1024

			// Calculate current requests (current replicas)
			dm.Requests.CPU = cpuMillis * int64(service.Instances)
			dm.Requests.Memory = memoryBytes * int64(service.Instances)

			// Calculate max requests (max replicas)
			dm.MaxRequests.CPU = cpuMillis * int64(maxReplicas)
			dm.MaxRequests.Memory = memoryBytes * int64(maxReplicas)

			deployments = append(deployments, dm)
		}
	}

	// Clear the progress indicator
	fmt.Fprintf(os.Stderr, "\r\033[K")

	return deployments, nil
}

func (c *PorterClient) ListApplications(ctx context.Context) ([]PorterApplication, error) {
	url := fmt.Sprintf("%s/api/v2/alpha/projects/%s/applications?limit=100", c.BaseURL, c.ProjectID)

	var response PorterListApplicationsResponse
	err := c.doAPIRequest(ctx, "GET", url, &response)
	if err != nil {
		return nil, err
	}

	return response.Applications, nil
}

func (c *PorterClient) GetApplication(ctx context.Context, appID string) (*PorterApplicationDetail, error) {
	url := fmt.Sprintf("%s/api/v2/alpha/projects/%s/applications/%s", c.BaseURL, c.ProjectID, appID)

	var app PorterApplicationDetail
	err := c.doAPIRequest(ctx, "GET", url, &app)
	if err != nil {
		return nil, err
	}

	return &app, nil
}

func (c *PorterClient) GetDeploymentTarget(ctx context.Context, targetID string) (*PorterDeploymentTarget, error) {
	// Load all deployment targets if not already loaded
	if !c.deploymentTargetsLoaded {
		if err := c.loadDeploymentTargets(ctx); err != nil {
			return nil, err
		}
	}

	// Return from cache
	if target, ok := c.deploymentTargetCache[targetID]; ok {
		return target, nil
	}

	return nil, fmt.Errorf("deployment target %s not found", targetID)
}

func (c *PorterClient) loadDeploymentTargets(ctx context.Context) error {
	url := fmt.Sprintf("%s/api/v2/projects/%s/deployment-targets", c.BaseURL, c.ProjectID)

	var response PorterListDeploymentTargetsResponse
	err := c.doAPIRequest(ctx, "GET", url, &response)
	if err != nil {
		return err
	}

	for i := range response.DeploymentTargets {
		target := &response.DeploymentTargets[i]
		c.deploymentTargetCache[target.ID] = target
	}

	c.deploymentTargetsLoaded = true
	return nil
}

func (c *PorterClient) GetCluster(ctx context.Context, clusterID int) (*PorterCluster, error) {
	// Load all clusters if not already loaded
	if !c.clustersLoaded {
		if err := c.loadClusters(ctx); err != nil {
			return nil, err
		}
	}

	// Return from cache
	if cluster, ok := c.clusterCache[clusterID]; ok {
		return cluster, nil
	}

	return nil, fmt.Errorf("cluster %d not found", clusterID)
}

func (c *PorterClient) loadClusters(ctx context.Context) error {
	url := fmt.Sprintf("%s/api/v2/projects/%s/clusters", c.BaseURL, c.ProjectID)

	var response PorterListClustersResponse
	err := c.doAPIRequest(ctx, "GET", url, &response)
	if err != nil {
		return err
	}

	for i := range response.Clusters {
		cluster := &response.Clusters[i]
		c.clusterCache[cluster.ID] = cluster
	}

	c.clustersLoaded = true
	return nil
}

func (c *PorterClient) doAPIRequest(ctx context.Context, method, url string, result interface{}) error {
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	if c.Debug {
		fmt.Fprintf(os.Stderr, "DEBUG - %s %s Raw Response:\n%s\n\n", method, url, string(body))
	}

	return json.Unmarshal(body, result)
}

func showProgress(stderr interface{ Write([]byte) (int, error) }, spinner string, current, total int, name string) {
	fmt.Fprintf(stderr, "\r%s Loading application %d/%d: %s...\033[K", spinner, current, total, name)
}

func clearProgress(stderr interface{ Write([]byte) (int, error) }) {
	fmt.Fprintf(stderr, "\r\033[K")
}
