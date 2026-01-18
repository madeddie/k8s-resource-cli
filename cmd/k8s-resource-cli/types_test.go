package main

import (
	"testing"
)

func TestOutputTypeConstants(t *testing.T) {
	if OutputTypeUsage == "" {
		t.Error("OutputTypeUsage should not be empty")
	}
	if OutputTypeRequests == "" {
		t.Error("OutputTypeRequests should not be empty")
	}
	if OutputTypeMaxRequests == "" {
		t.Error("OutputTypeMaxRequests should not be empty")
	}

	if OutputTypeUsage == OutputTypeRequests {
		t.Error("OutputTypeUsage and OutputTypeRequests should be different")
	}
	if OutputTypeUsage == OutputTypeMaxRequests {
		t.Error("OutputTypeUsage and OutputTypeMaxRequests should be different")
	}
	if OutputTypeRequests == OutputTypeMaxRequests {
		t.Error("OutputTypeRequests and OutputTypeMaxRequests should be different")
	}
}

func TestDeploymentMetricsStruct(t *testing.T) {
	dm := DeploymentMetrics{
		Name:            "test-deployment",
		Namespace:       "default",
		Type:            "Deployment",
		CurrentReplicas: 3,
		DesiredReplicas: 2,
		MaxReplicas:     5,
		Usage: ResourceMetrics{
			CPU:    500,
			Memory: 1024,
		},
		Requests: ResourceMetrics{
			CPU:    1000,
			Memory: 2048,
		},
		MaxRequests: ResourceMetrics{
			CPU:    2500,
			Memory: 5120,
		},
	}

	if dm.Name != "test-deployment" {
		t.Errorf("Name = %v, want test-deployment", dm.Name)
	}
	if dm.Namespace != "default" {
		t.Errorf("Namespace = %v, want default", dm.Namespace)
	}
	if dm.Type != "Deployment" {
		t.Errorf("Type = %v, want Deployment", dm.Type)
	}
	if dm.CurrentReplicas != 3 {
		t.Errorf("CurrentReplicas = %v, want 3", dm.CurrentReplicas)
	}
	if dm.DesiredReplicas != 2 {
		t.Errorf("DesiredReplicas = %v, want 2", dm.DesiredReplicas)
	}
	if dm.MaxReplicas != 5 {
		t.Errorf("MaxReplicas = %v, want 5", dm.MaxReplicas)
	}
}

func TestResourceMetricsStruct(t *testing.T) {
	rm := ResourceMetrics{
		CPU:    1000,
		Memory: 2048,
	}

	if rm.CPU != 1000 {
		t.Errorf("CPU = %v, want 1000", rm.CPU)
	}
	if rm.Memory != 2048 {
		t.Errorf("Memory = %v, want 2048", rm.Memory)
	}
}

func TestPorterApplicationStruct(t *testing.T) {
	app := PorterApplication{
		ID:   "app-123",
		Name: "my-app",
	}

	if app.ID != "app-123" {
		t.Errorf("ID = %v, want app-123", app.ID)
	}
	if app.Name != "my-app" {
		t.Errorf("Name = %v, want my-app", app.Name)
	}
}
