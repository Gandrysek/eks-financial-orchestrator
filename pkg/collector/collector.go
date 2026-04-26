package collector

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	metricsv1beta1 "k8s.io/metrics/pkg/client/clientset/versioned/typed/metrics/v1beta1"
)

// DefaultCostCollector implements the CostCollector interface, collecting
// cluster metrics from the Kubernetes Metrics API and correlating them
// with AWS cost data.
type DefaultCostCollector struct {
	kubeClient    kubernetes.Interface
	metricsClient metricsv1beta1.MetricsV1beta1Interface
	awsFetcher    *AWSCostFetcher
	cache         *CostCache
	curBucket     string
	curPrefix     string
	logger        logr.Logger
}

// CostCollectorConfig holds optional configuration for the DefaultCostCollector.
type CostCollectorConfig struct {
	AWSFetcher *AWSCostFetcher
	Cache      *CostCache
	CURBucket  string
	CURPrefix  string
}

// NewDefaultCostCollector creates a new DefaultCostCollector with the given
// Kubernetes client, metrics client, and logger. The optional config parameter
// provides AWS integration; if nil, AWS methods will return errors.
func NewDefaultCostCollector(
	kubeClient kubernetes.Interface,
	metricsClient metricsv1beta1.MetricsV1beta1Interface,
	logger logr.Logger,
	config ...*CostCollectorConfig,
) *DefaultCostCollector {
	c := &DefaultCostCollector{
		kubeClient:    kubeClient,
		metricsClient: metricsClient,
		logger:        logger.WithName("cost-collector"),
	}

	if len(config) > 0 && config[0] != nil {
		cfg := config[0]
		c.awsFetcher = cfg.AWSFetcher
		c.cache = cfg.Cache
		c.curBucket = cfg.CURBucket
		c.curPrefix = cfg.CURPrefix
	}

	return c
}

// CollectClusterMetrics gathers CPU, memory, network, and storage metrics
// from all nodes and pods in the cluster using the Kubernetes Metrics API.
func (c *DefaultCostCollector) CollectClusterMetrics(ctx context.Context) (*ClusterMetrics, error) {
	c.logger.V(1).Info("collecting cluster metrics")

	pods, err := c.collectPodMetrics(ctx)
	if err != nil {
		return nil, fmt.Errorf("collecting pod metrics: %w", err)
	}

	nodes, err := c.collectNodeMetrics(ctx)
	if err != nil {
		return nil, fmt.Errorf("collecting node metrics: %w", err)
	}

	metrics := &ClusterMetrics{
		Timestamp: time.Now().UTC(),
		Pods:      pods,
		Nodes:     nodes,
	}

	c.logger.Info("cluster metrics collected", "pods", len(pods), "nodes", len(nodes))
	return metrics, nil
}

// collectPodMetrics retrieves per-pod resource usage from the Metrics API
// and enriches it with pod metadata (labels, node name).
func (c *DefaultCostCollector) collectPodMetrics(ctx context.Context) ([]PodMetrics, error) {
	podMetricsList, err := c.metricsClient.PodMetricses("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing pod metrics: %w", err)
	}

	// Build a lookup of pod objects for labels and node assignment.
	podLookup := make(map[string]*corev1.Pod)
	namespaces, err := c.kubeClient.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing namespaces: %w", err)
	}
	for i := range namespaces.Items {
		ns := namespaces.Items[i].Name
		podList, err := c.kubeClient.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			c.logger.Error(err, "failed to list pods in namespace", "namespace", ns)
			continue
		}
		for j := range podList.Items {
			pod := &podList.Items[j]
			key := pod.Namespace + "/" + pod.Name
			podLookup[key] = pod
		}
	}

	var result []PodMetrics
	for i := range podMetricsList.Items {
		pm := &podMetricsList.Items[i]

		// Sum CPU and memory across all containers in the pod.
		var cpuCores float64
		var memBytes float64
		for j := range pm.Containers {
			container := &pm.Containers[j]
			cpuCores += quantityToFloat64Cores(container.Usage.Cpu())
			memBytes += quantityToFloat64Bytes(container.Usage.Memory())
		}

		// Look up the pod object for labels and node name.
		key := pm.Namespace + "/" + pm.Name
		var nodeName string
		var labels map[string]string
		if pod, ok := podLookup[key]; ok {
			nodeName = pod.Spec.NodeName
			labels = pod.Labels
		}

		result = append(result, PodMetrics{
			Name:      pm.Name,
			Namespace: pm.Namespace,
			NodeName:  nodeName,
			CPUUsage:  cpuCores,
			MemUsage:  memBytes,
			NetUsage:  0, // Network metrics require additional data sources
			Storage:   0, // Storage metrics require additional data sources
			Labels:    labels,
		})
	}

	return result, nil
}

// collectNodeMetrics retrieves per-node resource usage from the Metrics API
// and enriches it with node metadata (instance type, purchase option, capacity).
func (c *DefaultCostCollector) collectNodeMetrics(ctx context.Context) ([]NodeMetrics, error) {
	nodeMetricsList, err := c.metricsClient.NodeMetricses().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing node metrics: %w", err)
	}

	// Build a lookup of node objects for labels and capacity.
	nodeLookup := make(map[string]*corev1.Node)
	nodeList, err := c.kubeClient.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing nodes: %w", err)
	}
	for i := range nodeList.Items {
		node := &nodeList.Items[i]
		nodeLookup[node.Name] = node
	}

	var result []NodeMetrics
	for i := range nodeMetricsList.Items {
		nm := &nodeMetricsList.Items[i]

		cpuUsage := quantityToFloat64Cores(nm.Usage.Cpu())
		memUsage := quantityToFloat64Bytes(nm.Usage.Memory())

		var instanceType string
		var purchaseOption string
		var cpuCapacity float64
		var memCapacity float64
		var labels map[string]string

		if node, ok := nodeLookup[nm.Name]; ok {
			labels = node.Labels
			instanceType = node.Labels["node.kubernetes.io/instance-type"]
			purchaseOption = detectPurchaseOption(node)
			cpuCapacity = quantityToFloat64Cores(node.Status.Capacity.Cpu())
			memCapacity = quantityToFloat64Bytes(node.Status.Capacity.Memory())
		}

		result = append(result, NodeMetrics{
			Name:           nm.Name,
			InstanceType:   instanceType,
			PurchaseOption: purchaseOption,
			CPUCapacity:    cpuCapacity,
			CPUUsage:       cpuUsage,
			MemCapacity:    memCapacity,
			MemUsage:       memUsage,
			NetUsage:       0, // Network metrics require additional data sources
			Storage:        0, // Storage metrics require additional data sources
			Labels:         labels,
		})
	}

	return result, nil
}

// detectPurchaseOption determines the purchase option for a node by checking
// well-known labels and annotations. Returns "on_demand" as the default.
func detectPurchaseOption(node *corev1.Node) string {
	// Check custom label first (set by the orchestrator or cluster admin).
	if option, ok := node.Labels["finops.eks.io/purchase-option"]; ok {
		return option
	}

	// Check annotation as fallback.
	if option, ok := node.Annotations["finops.eks.io/purchase-option"]; ok {
		return option
	}

	// Check Karpenter capacity type label.
	if capacityType, ok := node.Labels["karpenter.sh/capacity-type"]; ok {
		switch capacityType {
		case "spot":
			return "spot"
		case "on-demand":
			return "on_demand"
		}
	}

	// Check EKS managed node group capacity type label.
	if capacityType, ok := node.Labels["eks.amazonaws.com/capacityType"]; ok {
		switch capacityType {
		case "SPOT":
			return "spot"
		case "ON_DEMAND":
			return "on_demand"
		}
	}

	return "on_demand"
}

// quantityToFloat64Cores converts a resource.Quantity representing CPU
// to float64 cores. For example, "500m" becomes 0.5.
func quantityToFloat64Cores(q *resource.Quantity) float64 {
	return float64(q.MilliValue()) / 1000.0
}

// quantityToFloat64Bytes converts a resource.Quantity representing memory
// to float64 bytes.
func quantityToFloat64Bytes(q *resource.Quantity) float64 {
	return float64(q.Value())
}

// FetchAWSCosts retrieves cost data from AWS Cost Explorer API.
// On failure it falls back to cached data and marks it as approximate.
func (c *DefaultCostCollector) FetchAWSCosts(ctx context.Context, start, end time.Time) (*AWSCostData, error) {
	if c.awsFetcher == nil {
		return nil, fmt.Errorf("AWS cost fetcher not configured")
	}

	data, err := c.awsFetcher.FetchCosts(ctx, start, end)
	if err != nil {
		c.logger.Error(err, "failed to fetch AWS costs, falling back to cache")

		// Fall back to cache if available.
		if c.cache != nil {
			if cached, ok := c.cache.Get(); ok {
				c.logger.Info("using cached cost data (approximate)")
				// Return a copy to avoid mutating shared cached state.
				approx := *cached
				approx.IsApproximate = true
				return &approx, nil
			}
		}

		return nil, fmt.Errorf("fetching AWS costs (no cache available): %w", err)
	}

	// Store successful result in cache.
	if c.cache != nil {
		c.cache.Set(data)
	}

	return data, nil
}

// FetchCURData retrieves detailed cost data from Cost and Usage Report.
func (c *DefaultCostCollector) FetchCURData(ctx context.Context, start, end time.Time) (*CURData, error) {
	if c.awsFetcher == nil {
		return nil, fmt.Errorf("AWS cost fetcher not configured")
	}

	if c.curBucket == "" {
		return nil, fmt.Errorf("CUR S3 bucket not configured")
	}

	return c.awsFetcher.FetchCUR(ctx, start, end, c.curBucket, c.curPrefix)
}

// CorrelateAndAggregate combines K8s metrics with AWS cost data and
// aggregates by namespace, service, and team. It distributes the total AWS
// cost across pods proportionally based on their resource usage (CPU + memory
// weighted at 50/50), then groups costs by namespace, service label, and team
// label.
func (c *DefaultCostCollector) CorrelateAndAggregate(ctx context.Context, metrics *ClusterMetrics, costs *AWSCostData) (*AggregatedCosts, error) {
	podCount := 0
	awsCost := 0.0
	if metrics != nil {
		podCount = len(metrics.Pods)
	}
	if costs != nil {
		awsCost = costs.TotalCost
	}

	c.logger.V(1).Info("correlating and aggregating costs",
		"pods", podCount,
		"awsTotalCost", awsCost,
	)

	result, err := correlateAndAggregate(ctx, metrics, costs)
	if err != nil {
		return nil, fmt.Errorf("correlating and aggregating: %w", err)
	}

	c.logger.Info("cost correlation complete",
		"namespaces", len(result.ByNamespace),
		"teams", len(result.ByTeam),
		"totalCost", result.TotalCost,
	)

	return result, nil
}

// AllocateSharedCosts distributes shared cluster costs (control plane,
// networking, system pods) proportionally across non-system namespaces
// based on their resource usage (DirectCost as a proxy).
func (c *DefaultCostCollector) AllocateSharedCosts(ctx context.Context, costs *AggregatedCosts) (*AggregatedCosts, error) {
	if costs == nil {
		return nil, fmt.Errorf("costs must not be nil")
	}

	c.logger.V(1).Info("allocating shared costs",
		"namespaces", len(costs.ByNamespace),
	)

	result := allocateSharedCosts(costs)

	c.logger.Info("shared cost allocation complete",
		"namespaces", len(result.ByNamespace),
		"teams", len(result.ByTeam),
		"totalCost", result.TotalCost,
	)

	return result, nil
}
