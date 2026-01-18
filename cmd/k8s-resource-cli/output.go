package main

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
)

func printResults(deployments []DeploymentMetrics, outputType string, usePorter bool, totalOnly bool) {
	if len(deployments) == 0 {
		fmt.Println("No deployments found")
		return
	}

	// Create a tabwriter for aligned output
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)

	// Check if there are any CronJobs in the list to determine if we need TYPE column
	hasCronJobs := false
	for _, dm := range deployments {
		if dm.Type == "CronJob" {
			hasCronJobs = true
			break
		}
	}

	// Print header (unless totalOnly is set)
	if !totalOnly {
		namespaceHeader := "NAMESPACE"
		if usePorter {
			namespaceHeader = "TARGET"
		}
		if hasCronJobs {
			fmt.Fprintf(w, "NAME\tTYPE\t%s\tREPLICAS\tCPU\tMEMORY\n", namespaceHeader)
		} else {
			fmt.Fprintf(w, "DEPLOYMENT\t%s\tREPLICAS\tCPU\tMEMORY\n", namespaceHeader)
		}
	}

	var totalCPU, totalMemory int64

	for _, dm := range deployments {
		var cpu, memory, replicas string

		switch outputType {
		case OutputTypeUsage, OutputTypeRequests:
			// Show current/max replicas
			replicas = fmt.Sprintf("%d/%d", dm.CurrentReplicas, dm.MaxReplicas)
		case OutputTypeMaxRequests:
			// Show only max replicas
			replicas = fmt.Sprintf("%d", dm.MaxReplicas)
		}

		switch outputType {
		case OutputTypeUsage:
			cpu = formatCPU(dm.Usage.CPU)
			memory = formatMemory(dm.Usage.Memory)
			totalCPU += dm.Usage.CPU
			totalMemory += dm.Usage.Memory
		case OutputTypeRequests:
			cpu = formatCPU(dm.Requests.CPU)
			memory = formatMemory(dm.Requests.Memory)
			totalCPU += dm.Requests.CPU
			totalMemory += dm.Requests.Memory
		case OutputTypeMaxRequests:
			if dm.MaxReplicas > dm.DesiredReplicas {
				// Has HPA, use max requests
				cpu = formatCPU(dm.MaxRequests.CPU)
				memory = formatMemory(dm.MaxRequests.Memory)
				totalCPU += dm.MaxRequests.CPU
				totalMemory += dm.MaxRequests.Memory
			} else {
				// No HPA, use current requests as max
				cpu = formatCPU(dm.Requests.CPU)
				memory = formatMemory(dm.Requests.Memory)
				totalCPU += dm.Requests.CPU
				totalMemory += dm.Requests.Memory
			}
		}

		// Only print individual lines if totalOnly is not set
		if !totalOnly {
			if hasCronJobs {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", dm.Name, dm.Type, dm.Namespace, replicas, cpu, memory)
			} else {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", dm.Name, dm.Namespace, replicas, cpu, memory)
			}
		}
	}

	// Print totals row
	if hasCronJobs {
		fmt.Fprintf(w, "TOTAL\t\t\t\t%s\t%s\n", formatCPU(totalCPU), formatMemory(totalMemory))
	} else {
		fmt.Fprintf(w, "TOTAL\t\t\t%s\t%s\n", formatCPU(totalCPU), formatMemory(totalMemory))
	}

	// Flush the writer to output everything
	w.Flush()
}

func getEnvDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func parseResourceValue(value string, isCPU bool) (int64, error) {
	if value == "" {
		return 0, nil
	}

	value = strings.TrimSpace(value)

	if isCPU {
		if strings.HasSuffix(value, "m") {
			var millis int64
			n, err := fmt.Sscanf(value, "%dm", &millis)
			if n == 0 {
				return 0, fmt.Errorf("invalid CPU value: %s", value)
			}
			return millis, err
		} else if strings.Contains(value, "core") {
			var cores float64
			n, err := fmt.Sscanf(value, "%f", &cores)
			if n == 0 {
				return 0, fmt.Errorf("invalid CPU value: %s", value)
			}
			return int64(cores * 1000), err
		} else {
			var cores float64
			n, err := fmt.Sscanf(value, "%f", &cores)
			if n == 0 {
				return 0, fmt.Errorf("invalid CPU value: %s", value)
			}
			return int64(cores * 1000), err
		}
	} else {
		value = strings.ToUpper(value)

		if strings.HasSuffix(value, "GI") {
			var gib float64
			n, err := fmt.Sscanf(value, "%fGI", &gib)
			if n == 0 {
				return 0, fmt.Errorf("invalid memory value: %s", value)
			}
			return int64(gib * 1024 * 1024 * 1024), err
		} else if strings.HasSuffix(value, "G") {
			var gb float64
			n, err := fmt.Sscanf(value, "%fG", &gb)
			if n == 0 {
				return 0, fmt.Errorf("invalid memory value: %s", value)
			}
			return int64(gb * 1000 * 1000 * 1000), err
		} else if strings.HasSuffix(value, "MI") {
			var mib float64
			n, err := fmt.Sscanf(value, "%fMI", &mib)
			if n == 0 {
				return 0, fmt.Errorf("invalid memory value: %s", value)
			}
			return int64(mib * 1024 * 1024), err
		} else if strings.HasSuffix(value, "M") {
			var mb float64
			n, err := fmt.Sscanf(value, "%fM", &mb)
			if n == 0 {
				return 0, fmt.Errorf("invalid memory value: %s", value)
			}
			return int64(mb * 1000 * 1000), err
		} else if strings.HasSuffix(value, "KI") {
			var kib float64
			n, err := fmt.Sscanf(value, "%fKI", &kib)
			if n == 0 {
				return 0, fmt.Errorf("invalid memory value: %s", value)
			}
			return int64(kib * 1024), err
		} else if strings.HasSuffix(value, "K") {
			var kb float64
			n, err := fmt.Sscanf(value, "%fK", &kb)
			if n == 0 {
				return 0, fmt.Errorf("invalid memory value: %s", value)
			}
			return int64(kb * 1000), err
		} else {
			var bytes int64
			n, err := fmt.Sscanf(value, "%d", &bytes)
			if n == 0 {
				return 0, fmt.Errorf("invalid memory value: %s", value)
			}
			return bytes, err
		}
	}
}

func formatCPU(milliCores int64) string {
	if milliCores >= 1000 {
		return fmt.Sprintf("%.2f cores", float64(milliCores)/1000.0)
	}
	return fmt.Sprintf("%dm", milliCores)
}

func formatMemory(bytes int64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
	)

	if bytes >= GB {
		return fmt.Sprintf("%.2f GB", float64(bytes)/float64(GB))
	} else if bytes >= MB {
		return fmt.Sprintf("%.2f MB", float64(bytes)/float64(MB))
	} else if bytes >= KB {
		return fmt.Sprintf("%.2f KB", float64(bytes)/float64(KB))
	}
	return fmt.Sprintf("%d B", bytes)
}
