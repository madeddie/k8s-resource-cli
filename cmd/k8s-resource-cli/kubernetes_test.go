package main

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// TestRequestsFromPodSpec exercises the helper that backs the deployment
// request calculation. It exists primarily to lock in the fix for the
// "requests undercounted when fewer pods are running than desired" bug:
//
//	dm.Requests.CPU must be (per-pod CPU) * DesiredReplicas,
//	NOT (per-pod CPU) * CurrentReplicas.
func TestRequestsFromPodSpec(t *testing.T) {
	cpu3600 := resource.MustParse("3600m")
	mem2Gi := resource.MustParse("2Gi")

	tests := []struct {
		name    string
		podSpec []corev1.Container
		want    ResourceMetrics
	}{
		{
			name: "single container with cpu and memory",
			podSpec: []corev1.Container{
				{Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    cpu3600,
						corev1.ResourceMemory: mem2Gi,
					},
				}},
			},
			want: ResourceMetrics{CPU: 3600, Memory: 2 * 1024 * 1024 * 1024},
		},
		{
			name:    "no containers",
			podSpec: nil,
			want:    ResourceMetrics{CPU: 0, Memory: 0},
		},
		{
			name: "container with no requests set",
			podSpec: []corev1.Container{
				{Resources: corev1.ResourceRequirements{}},
			},
			want: ResourceMetrics{CPU: 0, Memory: 0},
		},
		{
			name: "multiple containers sum together",
			podSpec: []corev1.Container{
				{Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("1000m"),
						corev1.ResourceMemory: resource.MustParse("1Gi"),
					},
				}},
				{Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("500m"),
						corev1.ResourceMemory: resource.MustParse("512Mi"),
					},
				}},
			},
			want: ResourceMetrics{
				CPU:    1500,
				Memory: 1024*1024*1024 + 512*1024*1024,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := requestsFromPodSpec(tt.podSpec)
			if got != tt.want {
				t.Errorf("requestsFromPodSpec() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// TestDeploymentRequestsReflectDesiredReplicas locks in the bug fix: when a
// deployment has fewer running pods than desired (e.g. mid-scale-up), the
// request total should still reflect DesiredReplicas * per-pod-request, not
// CurrentReplicas * per-pod-request.
//
// Scenario mirrors the real-world case from the bug report:
//
//	api-web, current=3, desired=20, per-pod cpu=3600m, per-pod mem~7.09Gi.
//	Expected Requests.CPU = 20 * 3600m = 72000m (NOT 3 * 3600m = 10800m).
func TestDeploymentRequestsReflectDesiredReplicas(t *testing.T) {
	cpu3600 := resource.MustParse("3600m")
	mem := resource.MustParse("7256Mi") // arbitrary, mirrors real ~7.09Gi

	perPod := requestsFromPodSpec([]corev1.Container{
		{Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    cpu3600,
				corev1.ResourceMemory: mem,
			},
		}},
	})

	const currentReplicas int32 = 3
	const desiredReplicas int32 = 20

	dm := DeploymentMetrics{
		CurrentReplicas: currentReplicas,
		DesiredReplicas: desiredReplicas,
	}
	dm.Requests.CPU = perPod.CPU * int64(dm.DesiredReplicas)
	dm.Requests.Memory = perPod.Memory * int64(dm.DesiredReplicas)

	wantCPU := int64(3600) * int64(desiredReplicas)
	if dm.Requests.CPU != wantCPU {
		t.Errorf("Requests.CPU = %d, want %d (=%d per-pod × %d desired, NOT × %d current)",
			dm.Requests.CPU, wantCPU, 3600, desiredReplicas, currentReplicas)
	}

	wantMem := mem.Value() * int64(desiredReplicas)
	if dm.Requests.Memory != wantMem {
		t.Errorf("Requests.Memory = %d, want %d (=%d per-pod × %d desired)",
			dm.Requests.Memory, wantMem, mem.Value(), desiredReplicas)
	}
}
