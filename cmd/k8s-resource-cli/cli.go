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
	var debug bool
	var showVersion bool
	var allNamespaces bool
	var labelSelector string
	var includeCronJobs bool
	var totalOnly bool

	// Default kubeconfig path: KUBECONFIG env var, then ~/.kube/config
	defaultKubeconfig := os.Getenv("KUBECONFIG")
	if defaultKubeconfig == "" {
		if home := os.Getenv("HOME"); home != "" {
			defaultKubeconfig = filepath.Join(home, ".kube", "config")
		}
	}

	flag.BoolVar(&showVersion, "version", false, "Show version and exit")
	flag.StringVar(&outputType, "output", OutputTypeRequests, "Output type: usage, requests, or max-requests")
	flag.StringVar(&namespace, "namespace", "", "Namespace (defaults to current context or 'default')")
	flag.StringVar(&deploymentName, "deployment", "", "Deployment name (defaults to all deployments)")
	flag.StringVar(&kubeconfig, "kubeconfig", defaultKubeconfig, "Path to kubeconfig file")
	flag.BoolVar(&usePorter, "porter", false, "Use Porter API instead of direct Kubernetes access")
	flag.StringVar(&porterToken, "porter-token", os.Getenv("PORTER_TOKEN"), "Porter API token (or set PORTER_TOKEN env var)")
	flag.StringVar(&porterProjectID, "porter-project-id", os.Getenv("PORTER_PROJECT_ID"), "Porter project ID (or set PORTER_PROJECT_ID env var)")
	flag.StringVar(&porterBaseURL, "porter-url", getEnvDefault("PORTER_BASE_URL", "https://dashboard.porter.run"), "Porter API base URL")
	flag.BoolVar(&debug, "debug", false, "Enable debug output")
	flag.BoolVar(&allNamespaces, "A", false, "List resources across all namespaces")
	flag.BoolVar(&allNamespaces, "all-namespaces", false, "List resources across all namespaces")
	flag.StringVar(&labelSelector, "l", "", "Label selector to filter deployments (e.g., 'app=myapp,env=prod')")
	flag.StringVar(&labelSelector, "selector", "", "Label selector to filter deployments (alias for -l)")
	flag.BoolVar(&includeCronJobs, "include-cronjobs", false, "Include CronJobs in the resource calculation")
	flag.BoolVar(&totalOnly, "total-only", false, "Show only the total line, hide individual resources")
	flag.Parse()

	// Handle version flag
	if showVersion {
		fmt.Println(version)
		os.Exit(0)
	}

	// Validate output type
	if outputType != OutputTypeUsage && outputType != OutputTypeRequests && outputType != OutputTypeMaxRequests {
		fmt.Fprintf(os.Stderr, "Error: Invalid output type '%s'. Must be 'usage', 'requests', or 'max-requests'\n", outputType)
		os.Exit(1)
	}

	// Validate mutually exclusive flags (Kubernetes mode only)
	if !usePorter && namespace != "" && allNamespaces {
		fmt.Fprintf(os.Stderr, "Error: --namespace and -A/--all-namespaces flags are mutually exclusive\n")
		os.Exit(1)
	}

	// Validate deployment name and label selector are mutually exclusive
	if !usePorter && deploymentName != "" && labelSelector != "" {
		fmt.Fprintf(os.Stderr, "Error: --deployment and -l/--selector flags are mutually exclusive\n")
		os.Exit(1)
	}

	ctx := context.Background()
	var deployments []DeploymentMetrics

	if usePorter {
		// Use Porter API
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
		// Use direct Kubernetes API
		// Build config from kubeconfig
		config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error building kubeconfig: %v\n", err)
			os.Exit(1)
		}

		// Create kubernetes clientset
		clientset, err := kubernetes.NewForConfig(config)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating Kubernetes client: %v\n", err)
			os.Exit(1)
		}

		// Create metrics clientset (for usage metrics)
		metricsClientset, err := versioned.NewForConfig(config)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating metrics client: %v\n", err)
			os.Exit(1)
		}

		// Handle all-namespaces flag or get namespace from kubeconfig
		if allNamespaces {
			// Empty string means all namespaces in Kubernetes API
			namespace = ""
		} else if namespace == "" {
			// Get namespace from kubeconfig if not specified
			namespace, err = getNamespaceFromKubeconfig(kubeconfig)
			if err != nil {
				namespace = "default"
			}
		}

		// Get deployments
		if deploymentName != "" {
			if allNamespaces {
				// When using -A, search all namespaces for the deployment
				deploymentList, err := clientset.AppsV1().Deployments("").List(ctx, metav1.ListOptions{})
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error listing deployments: %v\n", err)
					os.Exit(1)
				}
				found := false
				for _, deployment := range deploymentList.Items {
					if deployment.Name == deploymentName {
						found = true
						metrics, err := getDeploymentMetrics(ctx, clientset, metricsClientset, deployment.Namespace, deployment.Name)
						if err != nil {
							fmt.Fprintf(os.Stderr, "Warning: Error getting metrics for deployment %s in namespace %s: %v\n",
								deploymentName, deployment.Namespace, err)
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
				// Get specific deployment in specific namespace
				deployment, err := clientset.AppsV1().Deployments(namespace).Get(ctx, deploymentName, metav1.GetOptions{})
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error getting deployment %s: %v\n", deploymentName, err)
					os.Exit(1)
				}
				metrics, err := getDeploymentMetrics(ctx, clientset, metricsClientset, deployment.Namespace, deployment.Name)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error getting metrics for deployment %s: %v\n", deploymentName, err)
					os.Exit(1)
				}
				deployments = append(deployments, metrics)
			}
		} else {
			// Get all deployments in namespace
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
				metrics, err := getDeploymentMetrics(ctx, clientset, metricsClientset, deployment.Namespace, deployment.Name)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Warning: Error getting metrics for deployment %s: %v\n", deployment.Name, err)
					continue
				}
				deployments = append(deployments, metrics)
			}
		}

		// Get cronjobs if includeCronJobs flag is set
		if includeCronJobs {
			if deploymentName != "" {
				if allNamespaces {
					// When using -A, search all namespaces for the cronjob
					cronJobList, err := clientset.BatchV1().CronJobs("").List(ctx, metav1.ListOptions{})
					if err != nil {
						fmt.Fprintf(os.Stderr, "Error listing cronjobs: %v\n", err)
						os.Exit(1)
					}
					found := false
					for _, cronJob := range cronJobList.Items {
						if cronJob.Name == deploymentName {
							found = true
							metrics, err := getCronJobMetrics(ctx, clientset, metricsClientset, cronJob.Namespace, cronJob.Name)
							if err != nil {
								fmt.Fprintf(os.Stderr, "Warning: Error getting metrics for cronjob %s in namespace %s: %v\n",
									deploymentName, cronJob.Namespace, err)
								continue
							}
							deployments = append(deployments, metrics)
						}
					}
					if !found {
						fmt.Fprintf(os.Stderr, "Warning: No cronjob named %s found in any namespace\n", deploymentName)
					}
				} else {
					// Get specific cronjob in specific namespace
					cronJob, err := clientset.BatchV1().CronJobs(namespace).Get(ctx, deploymentName, metav1.GetOptions{})
					if err != nil {
						fmt.Fprintf(os.Stderr, "Warning: Error getting cronjob %s: %v\n", deploymentName, err)
					} else {
						metrics, err := getCronJobMetrics(ctx, clientset, metricsClientset, cronJob.Namespace, cronJob.Name)
						if err != nil {
							fmt.Fprintf(os.Stderr, "Warning: Error getting metrics for cronjob %s: %v\n", deploymentName, err)
						} else {
							deployments = append(deployments, metrics)
						}
					}
				}
			} else {
				// Get all cronjobs in namespace
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
					metrics, err := getCronJobMetrics(ctx, clientset, metricsClientset, cronJob.Namespace, cronJob.Name)
					if err != nil {
						fmt.Fprintf(os.Stderr, "Warning: Error getting metrics for cronjob %s: %v\n", cronJob.Name, err)
						continue
					}
					deployments = append(deployments, metrics)
				}
			}
		}
	}

	// Output results
	printResults(deployments, outputType, usePorter, totalOnly)
}
