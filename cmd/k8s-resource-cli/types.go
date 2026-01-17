package main

import (
	"net/http"
)

const (
	OutputTypeUsage       = "usage"
	OutputTypeRequests    = "requests"
	OutputTypeMaxRequests = "max-requests"
)

type ResourceMetrics struct {
	CPU    int64 // in millicores
	Memory int64 // in bytes
}

type DeploymentMetrics struct {
	Name            string
	Namespace       string
	Type            string // "Deployment" or "CronJob"
	CurrentReplicas int32
	DesiredReplicas int32
	MaxReplicas     int32
	Usage           ResourceMetrics
	Requests        ResourceMetrics
	MaxRequests     ResourceMetrics
}

// Porter API data structures
type PorterApplication struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type PorterListApplicationsResponse struct {
	Applications []PorterApplication `json:"applications"`
}

type PorterApplicationDetail struct {
	ID                 string          `json:"id"`
	Name               string          `json:"name"`
	DeploymentTargetID string          `json:"deployment_target_id"`
	Services           []PorterService `json:"services"`
}

type PorterService struct {
	Name         string             `json:"name"`
	Type         string             `json:"type"`
	CPUCores     float64            `json:"cpu_cores"`
	RAMMegabytes int64              `json:"ram_megabytes"`
	Instances    int32              `json:"instances"`
	Autoscaling  *PorterAutoscaling `json:"autoscaling,omitempty"`
}

type PorterAutoscaling struct {
	Enabled      bool  `json:"enabled"`
	MinInstances int32 `json:"min_instances"`
	MaxInstances int32 `json:"max_instances"`
}

type PorterDeploymentTarget struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	ClusterID     int    `json:"cluster_id"`
	CloudProvider string `json:"cloud_provider"`
	IsPreview     bool   `json:"is_preview"`
}

type PorterListDeploymentTargetsResponse struct {
	DeploymentTargets []PorterDeploymentTarget `json:"deployment_targets"`
}

type PorterCluster struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type PorterListClustersResponse struct {
	Clusters []PorterCluster `json:"clusters"`
}

type PorterClient struct {
	BaseURL                 string
	Token                   string
	ProjectID               string
	HTTPClient              *http.Client
	Debug                   bool
	deploymentTargetCache   map[string]*PorterDeploymentTarget
	deploymentTargetsLoaded bool
	clusterCache            map[int]*PorterCluster
	clustersLoaded          bool
}
