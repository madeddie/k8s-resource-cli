package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/metrics/pkg/client/clientset/versioned"
)

// version is set at build time using -ldflags
var version = "dev"

func runCLI() {
	var outputType string
	var namespace string
	var deploymentName string
	var kubeconfig string
	var usePorter bool
	var porterToken string
	var porterProjectID string
	var porterBaseURL string
	var usePrometheus bool
	var prometheusURL string
	var prometheusWindow string
	var debug bool
	var showVersion bool
	var allNamespaces bool
	var labelSelector string
	var includeCronJobs bool
	var totalOnly bool
	var format string
	var nodeFilter string

	// Default kubeconfig path: KUBECONFIG env var, then ~/.kube/config
	defaultKubeconfig := os.Getenv("KUBECONFIG")
	if defaultKubeconfig == "" {
		if home := os.Getenv("HOME"); home != "" {
			defaultKubeconfig = filepath.Join(home, ".kube", "config")
		}
	}

	flag.BoolVar(&showVersion, "version", false, "Show version and exit")
	flag.StringVar(&outputType, "output", OutputTypeRequests, "Output type: usage, requests, max-requests, or combined")
	flag.StringVar(&namespace, "namespace", "", "Namespace (defaults to current context or 'default')")
	flag.StringVar(&deploymentName, "deployment", "", "Deployment name (defaults to all deployments)")
	flag.StringVar(&kubeconfig, "kubeconfig", defaultKubeconfig, "Path to kubeconfig file")
	flag.BoolVar(&usePorter, "porter", false, "Use Porter API instead of direct Kubernetes access")
	flag.StringVar(&porterToken, "porter-token", os.Getenv("PORTER_TOKEN"), "Porter API token (or set PORTER_TOKEN env var)")
	flag.StringVar(&porterProjectID, "porter-project-id", os.Getenv("PORTER_PROJECT_ID"), "Porter project ID (or set PORTER_PROJECT_ID env var)")
	flag.StringVar(&porterBaseURL, "porter-url", getEnvDefault("PORTER_BASE_URL", "https://dashboard.porter.run"), "Porter API base URL")
	flag.BoolVar(&usePrometheus, "prometheus", false, "Use Prometheus as data source for historical usage")
	flag.StringVar(&prometheusURL, "prometheus-url", os.Getenv("PROMETHEUS_URL"), "Prometheus base URL (or set PROMETHEUS_URL env var)")
	flag.StringVar(&prometheusWindow, "window", "7d", "Lookback window for Prometheus queries (Prometheus duration, e.g. 1h, 7d, 30d)")
	flag.BoolVar(&debug, "debug", false, "Enable debug output")
	flag.BoolVar(&allNamespaces, "A", false, "List resources across all namespaces")
	flag.BoolVar(&allNamespaces, "all-namespaces", false, "List resources across all namespaces")
	flag.StringVar(&labelSelector, "l", "", "Label selector to filter deployments (e.g., 'app=myapp,env=prod')")
	flag.StringVar(&labelSelector, "selector", "", "Label selector to filter deployments (alias for -l)")
	flag.BoolVar(&includeCronJobs, "include-cronjobs", false, "Include CronJobs in the resource calculation")
	flag.BoolVar(&totalOnly, "total-only", false, "Show only the total line, hide individual resources")
	flag.StringVar(&format, "format", FormatTable, "Output format: table or markdown")
	flag.StringVar(&nodeFilter, "node", "", "Filter results to pods scheduled on this node")
	flag.Parse()

	// Handle version flag
	if showVersion {
		fmt.Println(version)
		os.Exit(0)
	}

	// Validate output type
	if outputType != OutputTypeUsage && outputType != OutputTypeRequests && outputType != OutputTypeMaxRequests && outputType != OutputTypeCombined {
		fmt.Fprintf(os.Stderr, "Error: Invalid output type '%s'. Must be 'usage', 'requests', 'max-requests', or 'combined'\n", outputType)
		os.Exit(1)
	}

	// Validate format
	if format != FormatTable && format != FormatMarkdown {
		fmt.Fprintf(os.Stderr, "Error: Invalid format '%s'. Must be 'table' or 'markdown'\n", format)
		os.Exit(1)
	}

	validateFlags(usePorter, usePrometheus, namespace, allNamespaces, deploymentName, labelSelector)

	ctx := context.Background()
	var deployments []DeploymentMetrics

	if usePrometheus {
		if prometheusURL == "" {
			fmt.Fprintf(os.Stderr, "Error: Prometheus URL required. Set PROMETHEUS_URL env var or use --prometheus-url flag\n")
			os.Exit(1)
		}
		if labelSelector != "" {
			fmt.Fprintf(os.Stderr, "Warning: -l/--selector flag is not supported in Prometheus mode, ignoring\n")
		}
		if includeCronJobs {
			fmt.Fprintf(os.Stderr, "Warning: --include-cronjobs flag is not supported in Prometheus mode, ignoring\n")
		}
		if nodeFilter != "" {
			fmt.Fprintf(os.Stderr, "Warning: --node flag is not supported in Prometheus mode, ignoring\n")
		}
		if outputType == OutputTypeCombined {
			fmt.Fprintf(os.Stderr, "Error: --output combined is not supported in Prometheus mode (no current-snapshot data)\n")
			os.Exit(1)
		}

		if allNamespaces {
			namespace = ""
		}

		promClient := &PrometheusClient{
			BaseURL:    prometheusURL,
			HTTPClient: &http.Client{},
			Debug:      debug,
		}

		var err error
		deployments, err = getPrometheusDeploymentMetrics(ctx, promClient, namespace, deploymentName, prometheusWindow)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting Prometheus metrics: %v\n", err)
			os.Exit(1)
		}
	} else if usePorter {
		if porterToken == "" {
			fmt.Fprintf(os.Stderr, "Error: Porter token required. Set PORTER_TOKEN env var or use --porter-token flag\n")
			os.Exit(1)
		}
		if porterProjectID == "" {
			fmt.Fprintf(os.Stderr, "Error: Porter project ID required. Set PORTER_PROJECT_ID env var or use --porter-project-id flag\n")
			os.Exit(1)
		}
		if labelSelector != "" {
			fmt.Fprintf(os.Stderr, "Warning: -l/--selector flag is only supported in Kubernetes mode, ignoring\n")
		}
		if includeCronJobs {
			fmt.Fprintf(os.Stderr, "Warning: --include-cronjobs flag is only supported in Kubernetes mode, ignoring\n")
		}
		if nodeFilter != "" {
			fmt.Fprintf(os.Stderr, "Warning: --node flag is only supported in Kubernetes mode, ignoring\n")
		}

		client := &PorterClient{
			BaseURL:               porterBaseURL,
			Token:                 porterToken,
			ProjectID:             porterProjectID,
			HTTPClient:            &http.Client{},
			Debug:                 debug,
			deploymentTargetCache: make(map[string]*PorterDeploymentTarget),
			clusterCache:          make(map[int]*PorterCluster),
		}

		var err error
		deployments, err = getPorterApplicationMetrics(ctx, client, deploymentName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting Porter application metrics: %v\n", err)
			os.Exit(1)
		}
	} else {
		clientset, metricsClientset := setupKubernetesClients(kubeconfig)

		if allNamespaces {
			namespace = ""
		} else if namespace == "" {
			var err error
			namespace, err = getNamespaceFromKubeconfig(kubeconfig)
			if err != nil {
				namespace = "default"
			}
		}

		deployments = getAllDeployments(ctx, clientset, metricsClientset, namespace, deploymentName, labelSelector, nodeFilter, allNamespaces)

		if includeCronJobs {
			cronJobDeployments := getAllCronJobs(ctx, clientset, metricsClientset, namespace, deploymentName, labelSelector, nodeFilter, allNamespaces)
			deployments = append(deployments, cronJobDeployments...)
		}
	}

	sourceMode := "k8s"
	if usePorter {
		sourceMode = "porter"
	} else if usePrometheus {
		sourceMode = "prometheus"
	}

	printResults(deployments, outputType, sourceMode, totalOnly, format)
}

func validateFlags(usePorter, usePrometheus bool, namespace string, allNamespaces bool, deploymentName string, labelSelector string) {
	if usePorter && usePrometheus {
		fmt.Fprintf(os.Stderr, "Error: --porter and --prometheus flags are mutually exclusive\n")
		os.Exit(1)
	}

	if namespace != "" && allNamespaces {
		fmt.Fprintf(os.Stderr, "Error: --namespace and -A/--all-namespaces flags are mutually exclusive\n")
		os.Exit(1)
	}

	if deploymentName != "" && labelSelector != "" {
		fmt.Fprintf(os.Stderr, "Error: --deployment and -l/--selector flags are mutually exclusive\n")
		os.Exit(1)
	}
}

func setupKubernetesClients(kubeconfig string) (*kubernetes.Clientset, *versioned.Clientset) {
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error building kubeconfig: %v\n", err)
		os.Exit(1)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating Kubernetes client: %v\n", err)
		os.Exit(1)
	}

	metricsClientset, err := versioned.NewForConfig(config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating metrics client: %v\n", err)
		os.Exit(1)
	}

	return clientset, metricsClientset
}

func getAllDeployments(ctx context.Context, clientset *kubernetes.Clientset, metricsClientset *versioned.Clientset, namespace, deploymentName, labelSelector, nodeFilter string, allNamespaces bool) []DeploymentMetrics {
	var deployments []DeploymentMetrics

	if deploymentName != "" {
		if allNamespaces {
			deploymentList, err := clientset.AppsV1().Deployments("").List(ctx, metav1.ListOptions{})
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error listing deployments: %v\n", err)
				os.Exit(1)
			}
			found := false
			for _, deployment := range deploymentList.Items {
				if deployment.Name == deploymentName {
					found = true
					metrics, err := getDeploymentMetrics(ctx, clientset, metricsClientset, deployment.Namespace, deployment.Name, nodeFilter)
					if err != nil {
						fmt.Fprintf(os.Stderr, "Warning: Error getting metrics for deployment %s in namespace %s: %v\n",
							deploymentName, deployment.Namespace, err)
						continue
					}
					if nodeFilter != "" && metrics.CurrentReplicas == 0 && metrics.Requests.CPU == 0 && metrics.Requests.Memory == 0 {
						continue
					}
					deployments = append(deployments, metrics)
				}
			}
			if !found {
				fmt.Fprintf(os.Stderr, "Error: No deployment named %s found in any namespace\n", deploymentName)
				os.Exit(1)
			}
		} else {
			deployment, err := clientset.AppsV1().Deployments(namespace).Get(ctx, deploymentName, metav1.GetOptions{})
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error getting deployment %s: %v\n", deploymentName, err)
				os.Exit(1)
			}
			metrics, err := getDeploymentMetrics(ctx, clientset, metricsClientset, deployment.Namespace, deployment.Name, nodeFilter)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error getting metrics for deployment %s: %v\n", deploymentName, err)
				os.Exit(1)
			}
			deployments = append(deployments, metrics)
		}
	} else {
		listOptions := metav1.ListOptions{}
		if labelSelector != "" {
			listOptions.LabelSelector = labelSelector
		}
		deploymentList, err := clientset.AppsV1().Deployments(namespace).List(ctx, listOptions)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error listing deployments: %v\n", err)
			os.Exit(1)
		}
		for _, deployment := range deploymentList.Items {
			metrics, err := getDeploymentMetrics(ctx, clientset, metricsClientset, deployment.Namespace, deployment.Name, nodeFilter)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: Error getting metrics for deployment %s: %v\n", deployment.Name, err)
				continue
			}
			if nodeFilter != "" && metrics.CurrentReplicas == 0 && metrics.Requests.CPU == 0 && metrics.Requests.Memory == 0 {
				continue
			}
			deployments = append(deployments, metrics)
		}
	}

	return deployments
}

func getAllCronJobs(ctx context.Context, clientset *kubernetes.Clientset, metricsClientset *versioned.Clientset, namespace, deploymentName, labelSelector, nodeFilter string, allNamespaces bool) []DeploymentMetrics {
	var deployments []DeploymentMetrics

	if deploymentName != "" {
		if allNamespaces {
			cronJobList, err := clientset.BatchV1().CronJobs("").List(ctx, metav1.ListOptions{})
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error listing cronjobs: %v\n", err)
				os.Exit(1)
			}
			found := false
			for _, cronJob := range cronJobList.Items {
				if cronJob.Name == deploymentName {
					found = true
					metrics, err := getCronJobMetrics(ctx, clientset, metricsClientset, cronJob.Namespace, cronJob.Name, nodeFilter)
					if err != nil {
						fmt.Fprintf(os.Stderr, "Warning: Error getting metrics for cronjob %s in namespace %s: %v\n",
							deploymentName, cronJob.Namespace, err)
						continue
					}
					if nodeFilter != "" && metrics.CurrentReplicas == 0 && metrics.Requests.CPU == 0 && metrics.Requests.Memory == 0 {
						continue
					}
					deployments = append(deployments, metrics)
				}
			}
			if !found {
				fmt.Fprintf(os.Stderr, "Warning: No cronjob named %s found in any namespace\n", deploymentName)
			}
		} else {
			cronJob, err := clientset.BatchV1().CronJobs(namespace).Get(ctx, deploymentName, metav1.GetOptions{})
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: Error getting cronjob %s: %v\n", deploymentName, err)
			} else {
				metrics, err := getCronJobMetrics(ctx, clientset, metricsClientset, cronJob.Namespace, cronJob.Name, nodeFilter)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Warning: Error getting metrics for cronjob %s: %v\n", deploymentName, err)
				} else {
					deployments = append(deployments, metrics)
				}
			}
		}
	} else {
		listOptions := metav1.ListOptions{}
		if labelSelector != "" {
			listOptions.LabelSelector = labelSelector
		}
		cronJobList, err := clientset.BatchV1().CronJobs(namespace).List(ctx, listOptions)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error listing cronjobs: %v\n", err)
			os.Exit(1)
		}
		for _, cronJob := range cronJobList.Items {
			metrics, err := getCronJobMetrics(ctx, clientset, metricsClientset, cronJob.Namespace, cronJob.Name, nodeFilter)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: Error getting metrics for cronjob %s: %v\n", cronJob.Name, err)
				continue
			}
			if nodeFilter != "" && metrics.CurrentReplicas == 0 && metrics.Requests.CPU == 0 && metrics.Requests.Memory == 0 {
				continue
			}
			deployments = append(deployments, metrics)
		}
	}

	return deployments
}
