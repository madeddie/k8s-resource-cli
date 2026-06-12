package main

import (
	"context"
	"fmt"
	"os"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/metrics/pkg/client/clientset/versioned"
)

func getNamespaceFromKubeconfig(kubeconfigPath string) (string, error) {
	config, err := clientcmd.LoadFromFile(kubeconfigPath)
	if err != nil {
		return "", err
	}

	if config.CurrentContext == "" {
		return "", fmt.Errorf("no current context")
	}

	context, ok := config.Contexts[config.CurrentContext]
	if !ok {
		return "", fmt.Errorf("current context not found")
	}

	if context.Namespace != "" {
		return context.Namespace, nil
	}

	return "default", nil
}

func getDeploymentMetrics(ctx context.Context, clientset *kubernetes.Clientset, metricsClientset *versioned.Clientset, namespace, name, nodeFilter string) (DeploymentMetrics, error) {
	// Get the deployment first to get replicas information
	deployment, err := clientset.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return DeploymentMetrics{}, fmt.Errorf("error getting deployment: %w", err)
	}

	dm := DeploymentMetrics{
		Name:            name,
		Namespace:       namespace,
		Type:            "Deployment",
		CurrentReplicas: deployment.Status.Replicas,
	}

	if deployment.Spec.Replicas != nil {
		dm.DesiredReplicas = *deployment.Spec.Replicas
		dm.MaxReplicas = *deployment.Spec.Replicas
	} else {
		dm.DesiredReplicas = 0
		dm.MaxReplicas = 0
	}

	// Get label selector from deployment
	var labelSelector string
	if deployment.Spec.Selector != nil {
		labelSelector = metav1.FormatLabelSelector(deployment.Spec.Selector)
	} else {
		labelSelector = fmt.Sprintf("app=%s", name)
	}

	// Build list options, optionally filtering by node
	listOptions := metav1.ListOptions{LabelSelector: labelSelector}
	if nodeFilter != "" {
		listOptions.FieldSelector = fmt.Sprintf("spec.nodeName=%s", nodeFilter)
	}

	// Get pods for this deployment
	pods, err := clientset.CoreV1().Pods(namespace).List(ctx, listOptions)
	if err != nil {
		return dm, fmt.Errorf("error listing pods: %w", err)
	}

	// When filtering by node, override replica counts to reflect only pods on that node
	if nodeFilter != "" {
		dm.CurrentReplicas = int32(len(pods.Items))
		dm.DesiredReplicas = int32(len(pods.Items))
		dm.MaxReplicas = int32(len(pods.Items))
	}

	// Build set of pod names on the node (used to filter metrics below)
	podNames := make(map[string]struct{}, len(pods.Items))
	for _, pod := range pods.Items {
		podNames[pod.Name] = struct{}{}
	}

	// Calculate requests from pod specs
	for _, pod := range pods.Items {
		for _, container := range pod.Spec.Containers {
			if cpu := container.Resources.Requests.Cpu(); cpu != nil {
				dm.Requests.CPU += cpu.MilliValue()
			}
			if memory := container.Resources.Requests.Memory(); memory != nil {
				dm.Requests.Memory += memory.Value()
			}
		}
	}

	// Get current usage from metrics API
	podMetricsList, err := metricsClientset.MetricsV1beta1().PodMetricses(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Error getting pod metrics: %v\n", err)
	} else {
		for _, podMetrics := range podMetricsList.Items {
			if nodeFilter != "" {
				if _, ok := podNames[podMetrics.Name]; !ok {
					continue
				}
			}
			for _, container := range podMetrics.Containers {
				if cpu := container.Usage.Cpu(); cpu != nil {
					dm.Usage.CPU += cpu.MilliValue()
				}
				if memory := container.Usage.Memory(); memory != nil {
					dm.Usage.Memory += memory.Value()
				}
			}
		}
	}

	// Get HPA information
	hpaList, err := clientset.AutoscalingV1().HorizontalPodAutoscalers(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Error listing HPA: %v\n", err)
	} else {
		for _, hpa := range hpaList.Items {
			if hpa.Spec.ScaleTargetRef.Name == name && hpa.Spec.ScaleTargetRef.Kind == "Deployment" {
				dm.MaxReplicas = hpa.Spec.MaxReplicas
				// Calculate max requests based on HPA max replicas
				if dm.MaxReplicas > dm.DesiredReplicas && len(pods.Items) > 0 {
					// Get requests per pod (average from current pods)
					requestsPerPod := ResourceMetrics{
						CPU:    dm.Requests.CPU / int64(len(pods.Items)),
						Memory: dm.Requests.Memory / int64(len(pods.Items)),
					}
					dm.MaxRequests.CPU = requestsPerPod.CPU * int64(dm.MaxReplicas)
					dm.MaxRequests.Memory = requestsPerPod.Memory * int64(dm.MaxReplicas)
				}
				break
			}
		}
	}

	return dm, nil
}

func getCronJobMetrics(ctx context.Context, clientset *kubernetes.Clientset, metricsClientset *versioned.Clientset, namespace, name, nodeFilter string) (DeploymentMetrics, error) {
	// Get the cronjob first to get job template information
	cronJob, err := clientset.BatchV1().CronJobs(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return DeploymentMetrics{}, fmt.Errorf("error getting cronjob: %w", err)
	}

	// For cronjobs, we look at the jobTemplate spec to understand resource requirements
	// We use parallelism and completions from the job template
	var desiredReplicas int32 = 1
	if cronJob.Spec.JobTemplate.Spec.Completions != nil {
		desiredReplicas = *cronJob.Spec.JobTemplate.Spec.Completions
	} else if cronJob.Spec.JobTemplate.Spec.Parallelism != nil {
		desiredReplicas = *cronJob.Spec.JobTemplate.Spec.Parallelism
	}

	// Count active jobs created by this cronjob
	var currentReplicas int32
	if cronJob.Status.Active != nil {
		for range cronJob.Status.Active {
			currentReplicas += desiredReplicas // Each active job contributes its parallelism
		}
	}

	dm := DeploymentMetrics{
		Name:            name,
		Namespace:       namespace,
		Type:            "CronJob",
		CurrentReplicas: currentReplicas,
		DesiredReplicas: desiredReplicas,
		MaxReplicas:     desiredReplicas, // CronJobs don't scale, max equals desired
	}

	// Calculate resource requests from the job template spec
	for _, container := range cronJob.Spec.JobTemplate.Spec.Template.Spec.Containers {
		if cpu := container.Resources.Requests.Cpu(); cpu != nil {
			dm.Requests.CPU += cpu.MilliValue() * int64(desiredReplicas)
		}
		if memory := container.Resources.Requests.Memory(); memory != nil {
			dm.Requests.Memory += memory.Value() * int64(desiredReplicas)
		}
	}

	// Get pods from active jobs created by this cronjob for usage metrics
	if len(cronJob.Status.Active) > 0 {
		// List all pods owned by jobs created by this cronjob
		for _, activeJob := range cronJob.Status.Active {
			podListOptions := metav1.ListOptions{
				LabelSelector: fmt.Sprintf("job-name=%s", activeJob.Name),
			}
			if nodeFilter != "" {
				podListOptions.FieldSelector = fmt.Sprintf("spec.nodeName=%s", nodeFilter)
			}
			pods, err := clientset.CoreV1().Pods(namespace).List(ctx, podListOptions)
			if err == nil {
				for _, pod := range pods.Items {
					podMetrics, err := metricsClientset.MetricsV1beta1().PodMetricses(namespace).Get(ctx, pod.Name, metav1.GetOptions{})
					if err == nil {
						for _, container := range podMetrics.Containers {
							if cpu := container.Usage.Cpu(); cpu != nil {
								dm.Usage.CPU += cpu.MilliValue()
							}
							if memory := container.Usage.Memory(); memory != nil {
								dm.Usage.Memory += memory.Value()
							}
						}
					}
				}
			}
		}
	}

	// For cronjobs, max requests equals current requests (no HPA)
	dm.MaxRequests.CPU = dm.Requests.CPU
	dm.MaxRequests.Memory = dm.Requests.Memory

	return dm, nil
}
