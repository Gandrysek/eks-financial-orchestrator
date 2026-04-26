package collector

import (
	"context"
	"time"
)

// ClusterMetrics holds per-pod and per-node resource usage metrics collected
// from the Kubernetes cluster.
type ClusterMetrics struct {
	Timestamp time.Time    `json:"timestamp"`
	Pods      []PodMetrics  `json:"pods"`
	Nodes     []NodeMetrics `json:"nodes"`
}

// PodMetrics holds resource usage metrics for a single pod.
type PodMetrics struct {
	Name      string  `json:"name"`
	Namespace string  `json:"namespace"`
	NodeName  string  `json:"node_name"`
	CPUUsage  float64 `json:"cpu_usage"`    // in cores
	MemUsage  float64 `json:"memory_usage"` // in bytes
	NetUsage  float64 `json:"network_usage"` // in bytes/sec
	Storage   float64 `json:"storage_usage"` // in bytes
	Labels    map[string]string `json:"labels,omitempty"`
}

// NodeMetrics holds resource usage metrics for a single node.
type NodeMetrics struct {
	Name           string  `json:"name"`
	InstanceType   string  `json:"instance_type"`
	PurchaseOption string  `json:"purchase_option"` // on_demand, spot, reserved, savings_plan
	CPUCapacity    float64 `json:"cpu_capacity"`
	CPUUsage       float64 `json:"cpu_usage"`
	MemCapacity    float64 `json:"memory_capacity"`
	MemUsage       float64 `json:"memory_usage"`
	NetUsage       float64 `json:"network_usage"`
	Storage        float64 `json:"storage_usage"`
	Labels         map[string]string `json:"labels,omitempty"`
}

// AWSCostData holds cost data retrieved from the AWS Cost Explorer API.
type AWSCostData struct {
	StartTime    time.Time          `json:"start_time"`
	EndTime      time.Time          `json:"end_time"`
	ByService    map[string]float64 `json:"by_service"`
	ByTag        map[string]float64 `json:"by_tag"`
	TotalCost    float64            `json:"total_cost"`
	IsApproximate bool             `json:"is_approximate"`
}

// CURData holds detailed cost data from the AWS Cost and Usage Report.
type CURData struct {
	StartTime  time.Time       `json:"start_time"`
	EndTime    time.Time       `json:"end_time"`
	LineItems  []CURLineItem   `json:"line_items"`
	TotalCost  float64         `json:"total_cost"`
}

// CURLineItem represents a single line item from the CUR.
type CURLineItem struct {
	ResourceID     string            `json:"resource_id"`
	ServiceCode    string            `json:"service_code"`
	UsageType      string            `json:"usage_type"`
	PurchaseOption string            `json:"purchase_option"`
	Cost           float64           `json:"cost"`
	Tags           map[string]string `json:"tags,omitempty"`
}

// AggregatedCosts represents costs aggregated by various dimensions.
type AggregatedCosts struct {
	Timestamp   time.Time                    `json:"timestamp"`
	ByNamespace map[string]*NamespaceCost    `json:"by_namespace"`
	ByTeam      map[string]*TeamCost         `json:"by_team"`
	TotalCost   float64                      `json:"total_cost"`
}

// NamespaceCost holds cost data for a single namespace.
type NamespaceCost struct {
	Namespace     string             `json:"namespace"`
	Team          string             `json:"team"`
	DirectCost    float64            `json:"direct_cost"`
	SharedCost    float64            `json:"shared_cost"`
	TotalCost     float64            `json:"total_cost"`
	ByService     map[string]float64 `json:"by_service"`
	ByPod         map[string]float64 `json:"by_pod"`
	IsApproximate bool              `json:"is_approximate"`
}

// TeamCost holds cost data for a single team.
type TeamCost struct {
	Team         string             `json:"team"`
	DirectCost   float64            `json:"direct_cost"`
	IndirectCost float64            `json:"indirect_cost"`
	TotalCost    float64            `json:"total_cost"`
	Namespaces   map[string]float64 `json:"namespaces"`
}

// DailyCostRecord is a single data point for forecasting.
type DailyCostRecord struct {
	Date       time.Time `json:"date"`
	Namespace  string    `json:"namespace"`
	TotalCost  float64   `json:"total_cost"`
	DayOfWeek  int       `json:"day_of_week"`
	DayOfMonth int       `json:"day_of_month"`
}

// CostCollector defines the interface for cost data collection.
type CostCollector interface {
	// CollectClusterMetrics gathers CPU, memory, network, storage metrics
	// from all nodes and pods in the cluster.
	CollectClusterMetrics(ctx context.Context) (*ClusterMetrics, error)

	// FetchAWSCosts retrieves cost data from AWS Cost Explorer API
	// for the specified time range.
	FetchAWSCosts(ctx context.Context, start, end time.Time) (*AWSCostData, error)

	// FetchCURData retrieves detailed cost data from Cost and Usage Report.
	FetchCURData(ctx context.Context, start, end time.Time) (*CURData, error)

	// CorrelateAndAggregate combines K8s metrics with AWS cost data
	// and aggregates by namespace, service, and team.
	CorrelateAndAggregate(ctx context.Context, metrics *ClusterMetrics, costs *AWSCostData) (*AggregatedCosts, error)

	// AllocateSharedCosts distributes shared cluster costs (control plane,
	// networking, system pods) proportionally across namespaces.
	AllocateSharedCosts(ctx context.Context, costs *AggregatedCosts) (*AggregatedCosts, error)
}
