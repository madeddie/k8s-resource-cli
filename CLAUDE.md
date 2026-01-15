# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is a Go CLI tool that retrieves resource requests and usage metrics for deployments. The tool supports two modes:

1. **Kubernetes Mode** (default): Directly interfaces with the Kubernetes API via kubeconfig
2. **Porter Mode** (`--porter` flag): Interfaces with the Porter API to retrieve application metrics

The tool supports three output modes: current usage, resource requests, and max requests (based on HPA/autoscaling configuration).

## Build and Development Commands

### Building the Project
```bash
go build -o k8s-resource-cli
```

### Installing
```bash
go install
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

### Testing with Porter
The tool requires:
- A Porter API token with viewer permissions
- A valid Porter project ID
- Access to the Porter API (default: https://dashboard.porter.run)

## Architecture

### Single-File Structure
The entire application is in `main.go`. All logic is contained in one file with no package separation.

### Core Data Structures

#### Shared Structures
**ResourceMetrics** - Represents CPU (millicores) and memory (bytes) metrics

**DeploymentMetrics** - Aggregates all metrics for a deployment including:
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
**main()** - Entry point; handles CLI flags, creates clients (Kubernetes or Porter), and orchestrates the flow

**printResults()** - Formats output using tabwriter with totals row
- Shows "NAMESPACE" column in Kubernetes mode
- Shows "TARGET" column in Porter mode

#### Kubernetes Mode Functions
**getNamespaceFromKubeconfig()** - Extracts the current namespace from kubeconfig context

**getDeploymentMetrics()** - Core function that:
1. Retrieves deployment spec for replica information
2. Lists pods using label selectors (tries deployment selector, falls back to `app=<name>`)
3. Calculates resource requests by summing container requests across all pods
4. Queries Metrics Server API for current usage
5. Looks up associated HPA to get max replicas and calculates max requests

#### Porter Mode Functions
**getPorterApplicationMetrics()** - Retrieves metrics from Porter API:
1. Lists all applications (with limit=100)
2. Shows progress spinner while loading each application
3. Fetches detailed application info for each app
4. Resolves deployment target and cluster names
5. Processes each service in the application
6. Calculates resources based on service CPU/memory and replica counts

**PorterClient.ListApplications()** - Lists applications from Porter API

**PorterClient.GetApplication()** - Fetches detailed application info by ID

**PorterClient.GetDeploymentTarget()** - Gets deployment target info (with caching)

**PorterClient.loadDeploymentTargets()** - Loads all deployment targets once and caches them

**PorterClient.GetCluster()** - Gets cluster info by ID (with caching)

**PorterClient.loadClusters()** - Loads all clusters once and caches them

### Output Modes

The tool has three output types controlled by the `--output` flag:

1. **usage** - Shows current CPU/memory usage from Metrics Server
2. **requests** - Shows resource requests from pod specs
3. **max-requests** - Shows projected resources at HPA max replicas (or current requests if no HPA)

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
