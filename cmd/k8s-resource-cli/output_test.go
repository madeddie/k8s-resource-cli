package main

import (
	"os"
	"testing"
)

func TestParseResourceValue(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		isCPU   bool
		want    int64
		wantErr bool
	}{
		{"CPU millicores", "1000m", true, 1000, false},
		{"CPU millicores short", "500m", true, 500, false},
		{"CPU cores decimal", "1.5", true, 1500, false},
		{"CPU cores integer", "2", true, 2000, false},
		{"CPU cores format", "2 cores", true, 2000, false},
		{"CPU cores format decimal", "1.5 cores", true, 1500, false},
		{"CPU empty string", "", true, 0, false},
		{"Memory Gi", "1Gi", false, 1073741824, false},
		{"Memory Gi decimal", "0.5Gi", false, 536870912, false},
		{"Memory G", "1G", false, 1000000000, false},
		{"Memory Mi", "256Mi", false, 268435456, false},
		{"Memory M", "512M", false, 512000000, false},
		{"Memory Ki", "100Ki", false, 102400, false},
		{"Memory K", "100K", false, 100000, false},
		{"Memory bytes", "1024", false, 1024, false},
		{"Memory bytes large", "1048576", false, 1048576, false},
		{"Memory lowercase mi", "256mi", false, 268435456, false},
		{"CPU invalid", "invalid", true, 0, true},
		{"CPU garbage", "notacpu", true, 0, true},
		{"Memory invalid", "notamemory", false, 0, true},
		{"Memory garbage", "xyz123", false, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseResourceValue(tt.value, tt.isCPU)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseResourceValue(%q, %v) error = %v, wantErr %v", tt.value, tt.isCPU, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("parseResourceValue(%q, %v) = %v, want %v", tt.value, tt.isCPU, got, tt.want)
			}
		})
	}
}

func TestFormatCPU(t *testing.T) {
	tests := []struct {
		name       string
		milliCores int64
		want       string
	}{
		{"zero", 0, "0m"},
		{"millicores", 500, "500m"},
		{"one core", 1000, "1.00 cores"},
		{"half core", 500, "500m"},
		{"two cores", 2000, "2.00 cores"},
		{"one and half cores", 1500, "1.50 cores"},
		{"fractional", 333, "333m"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatCPU(tt.milliCores); got != tt.want {
				t.Errorf("formatCPU(%v) = %v, want %v", tt.milliCores, got, tt.want)
			}
		})
	}
}

func TestFormatMemory(t *testing.T) {
	tests := []struct {
		name  string
		bytes int64
		want  string
	}{
		{"zero", 0, "0 B"},
		{"bytes", 500, "500 B"},
		{"kilobytes", 2048, "2.00 KB"},
		{"megabytes", 1048576, "1.00 MB"},
		{"gigabytes", 1073741824, "1.00 GB"},
		{"partial megabytes", 1572864, "1.50 MB"},
		{"partial gigabytes", 1610612736, "1.50 GB"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatMemory(tt.bytes); got != tt.want {
				t.Errorf("formatMemory(%v) = %v, want %v", tt.bytes, got, tt.want)
			}
		})
	}
}

func TestGetEnvDefault(t *testing.T) {
	tests := []struct {
		name       string
		envKey     string
		envValue   string
		defaultVal string
		want       string
		cleanup    func()
	}{
		{"returns env value", "TEST_ENV_VAR", "test-value", "default", "test-value", func() { os.Unsetenv("TEST_ENV_VAR") }},
		{"returns default when empty", "TEST_ENV_EMPTY", "", "default", "default", func() { os.Unsetenv("TEST_ENV_EMPTY") }},
		{"returns default when unset", "TEST_ENV_UNSET", "", "default", "default", func() { os.Unsetenv("TEST_ENV_UNSET") }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envValue != "" {
				os.Setenv(tt.envKey, tt.envValue)
			}
			defer tt.cleanup()

			if got := getEnvDefault(tt.envKey, tt.defaultVal); got != tt.want {
				t.Errorf("getEnvDefault(%q, %q) = %v, want %v", tt.envKey, tt.defaultVal, got, tt.want)
			}
		})
	}
}
