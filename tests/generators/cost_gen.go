package generators

import (
	"time"

	"github.com/eks-financial-orchestrator/pkg/collector"
	"pgregory.net/rapid"
)

// CostRecord generates a single DailyCostRecord with realistic values.
func CostRecord(t *rapid.T) collector.DailyCostRecord {
	year := rapid.IntRange(2023, 2025).Draw(t, "year")
	month := rapid.IntRange(1, 12).Draw(t, "month")
	day := rapid.IntRange(1, 28).Draw(t, "day")
	date := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)

	return collector.DailyCostRecord{
		Date:       date,
		Namespace:  rapid.StringMatching(`[a-z]{3,15}`).Draw(t, "namespace"),
		TotalCost:  rapid.Float64Range(0, 10000).Draw(t, "total_cost"),
		DayOfWeek:  int(date.Weekday()),
		DayOfMonth: date.Day(),
	}
}

// DailyCostHistory generates N days of sequential cost history for a single namespace.
func DailyCostHistory(t *rapid.T, days int) []collector.DailyCostRecord {
	ns := rapid.StringMatching(`[a-z]{3,15}`).Draw(t, "namespace")
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	records := make([]collector.DailyCostRecord, days)
	for i := 0; i < days; i++ {
		date := startDate.AddDate(0, 0, i)
		records[i] = collector.DailyCostRecord{
			Date:       date,
			Namespace:  ns,
			TotalCost:  rapid.Float64Range(0, 10000).Draw(t, "daily_cost"),
			DayOfWeek:  int(date.Weekday()),
			DayOfMonth: date.Day(),
		}
	}
	return records
}

// AggregatedCosts generates an AggregatedCosts struct with multiple namespaces.
func AggregatedCosts(t *rapid.T) *collector.AggregatedCosts {
	numNamespaces := rapid.IntRange(1, 5).Draw(t, "num_namespaces")
	byNamespace := make(map[string]*collector.NamespaceCost, numNamespaces)
	totalCost := 0.0

	for i := 0; i < numNamespaces; i++ {
		ns := rapid.StringMatching(`[a-z]{3,15}`).Draw(t, "ns_name")
		nsCost := NamespaceCost(t)
		nsCost.Namespace = ns
		byNamespace[ns] = nsCost
		totalCost += nsCost.TotalCost
	}

	return &collector.AggregatedCosts{
		Timestamp:   time.Now().UTC(),
		ByNamespace: byNamespace,
		ByTeam:      make(map[string]*collector.TeamCost),
		TotalCost:   totalCost,
	}
}

// NamespaceCost generates a NamespaceCost with realistic values.
func NamespaceCost(t *rapid.T) *collector.NamespaceCost {
	directCost := rapid.Float64Range(0, 5000).Draw(t, "direct_cost")
	sharedCost := rapid.Float64Range(0, 1000).Draw(t, "shared_cost")

	numServices := rapid.IntRange(1, 4).Draw(t, "num_services")
	byService := make(map[string]float64, numServices)
	for i := 0; i < numServices; i++ {
		svc := rapid.StringMatching(`[a-z]{3,10}`).Draw(t, "service_name")
		byService[svc] = rapid.Float64Range(0, 2000).Draw(t, "service_cost")
	}

	numPods := rapid.IntRange(1, 5).Draw(t, "num_pods")
	byPod := make(map[string]float64, numPods)
	for i := 0; i < numPods; i++ {
		pod := rapid.StringMatching(`[a-z]{3,10}-[a-z0-9]{5}`).Draw(t, "pod_name")
		byPod[pod] = rapid.Float64Range(0, 500).Draw(t, "pod_cost")
	}

	return &collector.NamespaceCost{
		Namespace:     rapid.StringMatching(`[a-z]{3,15}`).Draw(t, "namespace"),
		Team:          rapid.StringMatching(`[a-z]{3,10}`).Draw(t, "team"),
		DirectCost:    directCost,
		SharedCost:    sharedCost,
		TotalCost:     directCost + sharedCost,
		ByService:     byService,
		ByPod:         byPod,
		IsApproximate: rapid.Bool().Draw(t, "is_approximate"),
	}
}

// PodMetrics generates a PodMetrics struct with realistic resource usage values.
func PodMetrics(t *rapid.T) collector.PodMetrics {
	return collector.PodMetrics{
		Name:      rapid.StringMatching(`[a-z]{3,10}-[a-z0-9]{5}`).Draw(t, "pod_name"),
		Namespace: rapid.StringMatching(`[a-z]{3,15}`).Draw(t, "namespace"),
		NodeName:  rapid.StringMatching(`ip-[0-9]{1,3}-[0-9]{1,3}-[0-9]{1,3}-[0-9]{1,3}`).Draw(t, "node_name"),
		CPUUsage:  rapid.Float64Range(0, 64).Draw(t, "cpu_usage"),           // cores
		MemUsage:  rapid.Float64Range(0, 256e9).Draw(t, "memory_usage"),     // bytes (up to 256 GB)
		NetUsage:  rapid.Float64Range(0, 10e9).Draw(t, "network_usage"),     // bytes/sec
		Storage:   rapid.Float64Range(0, 1e12).Draw(t, "storage_usage"),     // bytes (up to 1 TB)
		Labels:    map[string]string{"app": rapid.StringMatching(`[a-z]{3,10}`).Draw(t, "app_label")},
	}
}

// NodeMetrics generates a NodeMetrics struct with realistic values.
func NodeMetrics(t *rapid.T) collector.NodeMetrics {
	purchaseOptions := []string{"on_demand", "spot", "reserved", "savings_plan"}
	instanceTypes := []string{"m5.large", "m5.xlarge", "c5.large", "c5.xlarge", "r5.large", "t3.medium"}

	cpuCap := rapid.Float64Range(2, 96).Draw(t, "cpu_capacity")
	memCap := rapid.Float64Range(4e9, 768e9).Draw(t, "memory_capacity")

	return collector.NodeMetrics{
		Name:           rapid.StringMatching(`ip-[0-9]{1,3}-[0-9]{1,3}-[0-9]{1,3}-[0-9]{1,3}`).Draw(t, "node_name"),
		InstanceType:   instanceTypes[rapid.IntRange(0, len(instanceTypes)-1).Draw(t, "instance_type_idx")],
		PurchaseOption: purchaseOptions[rapid.IntRange(0, len(purchaseOptions)-1).Draw(t, "purchase_option_idx")],
		CPUCapacity:    cpuCap,
		CPUUsage:       rapid.Float64Range(0, cpuCap).Draw(t, "cpu_usage"),
		MemCapacity:    memCap,
		MemUsage:       rapid.Float64Range(0, memCap).Draw(t, "memory_usage"),
		NetUsage:       rapid.Float64Range(0, 10e9).Draw(t, "network_usage"),
		Storage:        rapid.Float64Range(0, 1e12).Draw(t, "storage_usage"),
		Labels:         map[string]string{"node-role": rapid.StringMatching(`[a-z]{3,10}`).Draw(t, "role_label")},
	}
}

// ClusterMetrics generates a ClusterMetrics struct with random pods and nodes.
func ClusterMetrics(t *rapid.T) *collector.ClusterMetrics {
	numPods := rapid.IntRange(1, 10).Draw(t, "num_pods")
	pods := make([]collector.PodMetrics, numPods)
	for i := 0; i < numPods; i++ {
		pods[i] = PodMetrics(t)
	}

	numNodes := rapid.IntRange(1, 5).Draw(t, "num_nodes")
	nodes := make([]collector.NodeMetrics, numNodes)
	for i := 0; i < numNodes; i++ {
		nodes[i] = NodeMetrics(t)
	}

	return &collector.ClusterMetrics{
		Timestamp: time.Now().UTC(),
		Pods:      pods,
		Nodes:     nodes,
	}
}

// AWSCostData generates an AWSCostData struct with realistic values.
func AWSCostData(t *rapid.T) *collector.AWSCostData {
	services := []string{"AmazonEC2", "AmazonEKS", "AmazonS3", "AmazonRDS", "AmazonCloudWatch"}
	numServices := rapid.IntRange(1, len(services)).Draw(t, "num_services")
	byService := make(map[string]float64, numServices)
	totalCost := 0.0
	for i := 0; i < numServices; i++ {
		cost := rapid.Float64Range(0, 5000).Draw(t, "service_cost")
		byService[services[i]] = cost
		totalCost += cost
	}

	numTags := rapid.IntRange(0, 3).Draw(t, "num_tags")
	byTag := make(map[string]float64, numTags)
	for i := 0; i < numTags; i++ {
		tag := rapid.StringMatching(`[a-z]{3,10}`).Draw(t, "tag_key")
		byTag[tag] = rapid.Float64Range(0, 2000).Draw(t, "tag_cost")
	}

	start := time.Now().UTC().AddDate(0, 0, -1)
	end := time.Now().UTC()

	return &collector.AWSCostData{
		StartTime:     start,
		EndTime:       end,
		ByService:     byService,
		ByTag:         byTag,
		TotalCost:     totalCost,
		IsApproximate: rapid.Bool().Draw(t, "is_approximate"),
	}
}
