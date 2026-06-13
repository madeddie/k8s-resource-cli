# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Go CLI tool that retrieves resource requests and usage metrics for Kubernetes deployments and cronjobs. Two operating modes:

1. **Kubernetes mode** (default): reads from kubeconfig, calls Kubernetes API and Metrics Server directly
2. **Porter mode** (`--porter`): calls the Porter REST API to retrieve application metrics

Three output types: `usage` (Metrics Server), `requests` (pod specs), `max-requests` (requests × HPA max replicas).

## Build and Test Commands

```bash
# Build
go build -o k8s-resource-cli ./cmd/k8s-resource-cli

# Build with version info
go build -ldflags="-X main.version=$(git describe --tags --always)" -o k8s-resource-cli ./cmd/k8s-resource-cli

# Run all tests
go test ./...

# Run a specific test
go test -v -run TestFunctionName ./...

# Lint and format
gofmt -w .
go vet ./...
```

## Architecture

All source lives in `cmd/k8s-resource-cli/` as a single `main` package — functions and types are shared directly across files without imports.

**File responsibilities:**
- `cli.go` — flag parsing, client initialization, orchestration
- `types.go` — all data structures (`ResourceMetrics`, `DeploymentMetrics`, Porter types)
- `kubernetes.go` — Kubernetes API interactions (deployments, cronjobs, metrics)
- `porter.go` — Porter REST API client with in-memory caching
- `output.go` — tabwriter formatting and resource value parsing

### Data flow

```
runCLI() [cli.go]
  ├── Kubernetes mode → getDeploymentMetrics() + getCronJobMetrics() [kubernetes.go]
  └── Porter mode    → getPorterApplicationMetrics() [porter.go]
                           ↓
                     []DeploymentMetrics → printResults() [output.go]
```

`DeploymentMetrics` is the shared return type for both modes. It uses `Namespace` (Kubernetes) or `Target` (Porter) for the location column, and `Type` ("Deployment"/"CronJob") only when `--include-cronjobs` is active.

### Key design decisions

- **Pod label fallback**: `getDeploymentMetrics()` tries the deployment's real selector labels first, then falls back to `app=<name>`.
- **HPA max requests**: average requests per pod × HPA max replicas; equals current requests when no HPA exists.
- **Porter caching**: deployment targets and clusters are fetched once and stored in `PorterClient` struct fields to avoid redundant API calls.
- **Output separation**: data goes to stdout, progress/debug output goes to stderr.

### Kubeconfig resolution order

1. `--kubeconfig` flag
2. `KUBECONFIG` env var
3. `~/.kube/config`

### Environment variables

| Variable | Used by |
|---|---|
| `PORTER_TOKEN` | Porter auth |
| `PORTER_PROJECT_ID` | Porter project |
| `PORTER_BASE_URL` | Porter API base (default: `https://dashboard.porter.run`) |
| `KUBECONFIG` | Kubernetes config path |

## Output format changes

When `--include-cronjobs` is active, the header column changes from `DEPLOYMENT` to `NAME` and a `TYPE` column is added. When `--total-only` is set, individual rows are suppressed and only the `TOTAL` line is printed. Porter mode shows `TARGET` instead of `NAMESPACE`.
