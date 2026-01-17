# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is a Go CLI tool that retrieves resource requests and usage metrics for deployments and cronjobs. The tool supports two modes:

1. **Kubernetes Mode** (default): Directly interfaces with the Kubernetes API via kubeconfig
2. **Porter Mode** (`--porter` flag): Interfaces with the Porter API to retrieve application metrics

The tool supports three output modes: current usage, resource requests, and max requests (based on HPA/autoscaling configuration). In Kubernetes mode, CronJobs can be included in the calculation using the `--include-cronjobs` flag.

## Build and Development Commands

### Building the Project
```bash
go build -o k8s-resource-cli ./cmd/k8s-resource-cli
```

### Installing
```bash
go install ./cmd/k8s-resource-cli
```

### Running the Tool

#### Kubernetes Mode (Default)
```bash
# Default: show resource requests for all deployments
./k8s-resource-cli

# Show current usage
./k8s-resource-cli --output usage

# Show max requests based on HPA
./k8s-resource-cli --output max-requests

# Filter by namespace or deployment
./k8s-resource-cli --namespace production --deployment my-app

# Show resources across all namespaces
./k8s-resource-cli -A

# Use custom kubeconfig
./k8s-resource-cli --kubeconfig /path/to/config

# Include CronJobs in the resource calculation
./k8s-resource-cli --include-cronjobs

# Show all deployments and cronjobs across all namespaces
./k8s-resource-cli -A --include-cronjobs

# Filter by label selector and include cronjobs
./k8s-resource-cli -l app=myapp --include-cronjobs

# Show only the total line (hide individual deployments)
./k8s-resource-cli --total-only

# Show total across all namespaces
./k8s-resource-cli -A --total-only
```

#### Porter Mode
```bash
# Show resource requests for all Porter applications
./k8s-resource-cli --porter --porter-project-id 12345

# Filter by specific application
./k8s-resource-cli --porter --porter-project-id 12345 --deployment my-app

# Use custom Porter API URL
./k8s-resource-cli --porter --porter-project-id 12345 --porter-url https://custom.porter.run

# Enable debug mode to see raw API responses
./k8s-resource-cli --porter --porter-project-id 12345 --debug
```

#### Environment Variables
```bash
# Porter authentication
export PORTER_TOKEN=your_token_here
export PORTER_PROJECT_ID=12345
export PORTER_BASE_URL=https://dashboard.porter.run  # optional

# Kubernetes
export KUBECONFIG=/path/to/kubeconfig
```

### Testing with Kubernetes
The tool requires:
- A running Kubernetes cluster accessible via kubeconfig
- Metrics Server installed (for usage metrics)
- Deployments with resource requests configured
- CronJobs with resource requests configured (when using --include-cronjobs)

### Testing with Porter
The tool requires:
- A Porter API token with viewer permissions
- A valid Porter project ID
- Access to the Porter API (default: https://dashboard.porter.run)

## Architecture

### Project Structure
The application follows the Go project layout convention with the main package split into multiple focused files:

```
k8s-resource-cli/
├── cmd/
│   └── k8s-resource-cli/
│       ├── main.go          # Entry point (~5 lines)
│       ├── cli.go           # CLI orchestration (~280 lines)
│       ├── types.go         # Data structures (~80 lines)
│       ├── kubernetes.go    # K8s integration (~220 lines)
│       ├── porter.go        # Porter API (~350 lines)
│       └── output.go        # Formatting (~180 lines)
├── go.mod
├── go.sum
├── README.md
├── CLAUDE.md
└── LICENSE
```

All files are in the `main` package, so functions and types are shared directly without imports. This provides modularity while maintaining a simple single-package structure.

**File responsibilities:**
- `main.go` - Minimal entry point that calls `runCLI()`
- `cli.go` - Flag parsing, client initialization, and orchestration logic
- `types.go` - All data structures (ResourceMetrics, DeploymentMetrics, Porter types)
- `kubernetes.go` - Kubernetes API interactions (deployments, cronjobs, metrics)
- `porter.go` - Porter API client and methods
- `output.go` - Output formatting and resource parsing utilities

### Core Data Structures

#### Shared Structures
**ResourceMetrics** - Represents CPU (millicores) and memory (bytes) metrics

**DeploymentMetrics** - Aggregates all metrics for a deployment or cronjob including:
- Type field to distinguish between "Deployment" and "CronJob"
- Current/desired/max replica counts
- Usage, requests, and max requests calculations
- Namespace (Kubernetes) or Target (Porter) name

#### Porter-Specific Structures
**PorterClient** - HTTP client for Porter API with caching:
- Deployment target cache (avoids redundant API calls)
- Cluster cache (for name resolution)

**PorterApplication** - Basic application info from list endpoint

**PorterApplicationDetail** - Full application details including services

**PorterService** - Service configuration with CPU, memory, instances, and autoscaling

**PorterDeploymentTarget** - Cluster deployment target information

**PorterCluster** - Cluster name and ID mapping

### Key Functions

#### Shared Functions
**main()** (`main.go`) - Minimal entry point that calls `runCLI()`

**runCLI()** (`cli.go`) - Main orchestration function; handles CLI flags, creates clients (Kubernetes or Porter), and orchestrates the flow

**printResults()** (`output.go`) - Formats output using tabwriter with totals row
- Shows "NAMESPACE" column in Kubernetes mode
- Shows "TARGET" column in Porter mode
- Adds a "TYPE" column when CronJobs are included to distinguish between Deployments and CronJobs
- Changes header from "DEPLOYMENT" to "NAME" when TYPE column is present

#### Kubernetes Mode Functions
**getNamespaceFromKubeconfig()** (`kubernetes.go`) - Extracts the current namespace from kubeconfig context

**getDeploymentMetrics()** (`kubernetes.go`) - Core function that:
1. Retrieves deployment spec for replica information
2. Lists pods using label selectors (tries deployment selector, falls back to `app=<name>`)
3. Calculates resource requests by summing container requests across all pods
4. Queries Metrics Server API for current usage
5. Looks up associated HPA to get max replicas and calculates max requests

**getCronJobMetrics()** (`kubernetes.go`) - Similar to getDeploymentMetrics but for CronJobs:
1. Retrieves cronjob spec to get jobTemplate information
2. Uses jobTemplate's completions and parallelism for desired replicas
3. Counts active jobs created by the cronjob for current replicas
4. Calculates resource requests from the jobTemplate spec
5. Queries Metrics Server API for current usage from active job pods
6. For CronJobs, max requests equals current requests (CronJobs don't have HPA)

#### Porter Mode Functions
**getPorterApplicationMetrics()** (`porter.go`) - Retrieves metrics from Porter API:
1. Lists all applications (with limit=100)
2. Shows progress spinner while loading each application
3. Fetches detailed application info for each app
4. Resolves deployment target and cluster names
5. Processes each service in the application
6. Calculates resources based on service CPU/memory and replica counts

**PorterClient.ListApplications()** (`porter.go`) - Lists applications from Porter API

**PorterClient.GetApplication()** (`porter.go`) - Fetches detailed application info by ID

**PorterClient.GetDeploymentTarget()** (`porter.go`) - Gets deployment target info (with caching)

**PorterClient.loadDeploymentTargets()** (`porter.go`) - Loads all deployment targets once and caches them

**PorterClient.GetCluster()** (`porter.go`) - Gets cluster info by ID (with caching)

**PorterClient.loadClusters()** (`porter.go`) - Loads all clusters once and caches them

### Output Modes

The tool has three output types controlled by the `--output` flag:

1. **usage** - Shows current CPU/memory usage from Metrics Server
2. **requests** - Shows resource requests from pod specs
3. **max-requests** - Shows projected resources at HPA max replicas (or current requests if no HPA)

### Total Only Mode

The `--total-only` flag hides individual deployment/cronjob lines and shows only the TOTAL row. This is useful for:
- Quick summaries of total cluster resources
- Scripting and monitoring dashboards
- Getting aggregate metrics without detailed breakdown

Example output:
```
TOTAL     1.70 cores    2.49 GB
```

### Label Selection Strategy

The tool finds pods for a deployment using this fallback approach:
1. First attempts to use the deployment's actual selector labels via `metav1.FormatLabelSelector()`
2. Falls back to `app=<deployment-name>` if selector is nil

This handles different Kubernetes labeling conventions.

### Kubeconfig Resolution Order

1. `-kubeconfig` CLI flag (highest priority)
2. `KUBECONFIG` environment variable
3. `~/.kube/config` (default)

### HPA Max Requests Calculation

For deployments with HPA:
- Calculates average requests per pod from current pods
- Multiplies by HPA max replicas to get max requests
- If no HPA exists, max requests equals current requests

### Porter-Specific Features

#### Progress Spinner
When loading Porter applications, a spinner is displayed with progress:
```
⠹ Loading application 5/10: my-app...
```
- Uses Unicode braille characters (⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏)
- Updates in place using carriage returns
- Clears completely when done

#### Deployment Target Name Resolution
Porter deployment targets follow the pattern `clustername-nodegroupname`:
1. Fetches deployment target info by ID
2. Fetches cluster info using cluster_id from deployment target
3. If deployment target name doesn't include cluster name (e.g., "default"), prefixes with cluster name
4. Result: "default" becomes "edwinh-cluster-6c6blr-default"

#### API Caching
To optimize performance, the Porter client caches:
- **Deployment Targets**: Loaded once via `/api/v2/projects/{id}/deployment-targets`
- **Clusters**: Loaded once via `/api/v2/projects/{id}/clusters`
- Both are cached in memory for the duration of the command execution

#### Debug Mode
The `-debug` flag enables raw API response logging:
- `DEBUG - ListApplications Raw Response:`
- `DEBUG - GetApplication({id}) Raw Response:`
- `DEBUG - ListDeploymentTargets Raw Response:`
- `DEBUG - ListClusters Raw Response:`

All debug output goes to stderr, keeping stdout clean for the data table.

### Dependencies

Kubernetes dependencies (used only in Kubernetes mode):
- `k8s.io/client-go` - Kubernetes API client
- `k8s.io/api` - API type definitions
- `k8s.io/metrics` - Metrics Server client
- `k8s.io/apimachinery` - API machinery utilities

Standard library (used in both modes):
- `net/http` - HTTP client for Porter API
- `encoding/json` - JSON parsing
- `text/tabwriter` - Formatted output
- `flag` - Command-line flags

Uses Go 1.24+ and Kubernetes API version v0.29.0.

### CronJobs Support

The tool can include Kubernetes CronJobs in the resource calculation using the `--include-cronjobs` flag. This feature is only available in Kubernetes mode.

#### Key Differences for CronJobs:
- **Replicas**: CronJobs use the jobTemplate's `spec.completions` or `spec.parallelism` for desired replicas per job
- **Current Replicas**: Calculated by counting active jobs and their parallelism
- **No HPA**: CronJobs don't support HorizontalPodAutoscalers, so max requests always equal current requests
- **Resource Calculation**: Resources are calculated from the jobTemplate spec, representing what each job run would consume

#### Output with CronJobs:
When CronJobs are included (using `--include-cronjobs`), the output format changes:
- Header changes from "DEPLOYMENT" to "NAME"
- A new "TYPE" column is added showing either "Deployment" or "CronJob"
- Both deployments and cronjobs are included in the TOTAL row

#### Example Output:
```
NAME               TYPE         NAMESPACE   REPLICAS   CPU           MEMORY
my-app             Deployment   default     2/5        200m          512.00 MB
batch-processor    CronJob      default     3/3        1.50 cores    2.00 GB
TOTAL                                                  1.70 cores    2.49 GB
```
