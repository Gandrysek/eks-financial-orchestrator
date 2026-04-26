package collector

import (
	"context"
	"time"
)

// correlateAndAggregate is the internal implementation of CorrelateAndAggregate.
// It distributes the total AWS cost across pods proportionally based on their
// resource usage (CPU + memory weighted), then aggregates by namespace, service,
// and team.
func correlateAndAggregate(ctx context.Context, metrics *ClusterMetrics, costs *AWSCostData) (*AggregatedCosts, error) {
	if metrics == nil {
		metrics = &ClusterMetrics{}
	}
	if costs == nil {
		costs = &AWSCostData{}
	}

	totalAWSCost := costs.TotalCost

	// Calculate total cluster CPU and memory usage across all pods.
	var totalCPU, totalMem float64
	for i := range metrics.Pods {
		totalCPU += metrics.Pods[i].CPUUsage
		totalMem += metrics.Pods[i].MemUsage
	}

	podCount := len(metrics.Pods)

	// Calculate per-pod cost based on resource weight.
	// Weight = (podCPU / totalCPU) * 0.5 + (podMem / totalMem) * 0.5
	// Edge case: if total CPU or memory is zero, distribute evenly.
	podCosts := make(map[string]float64, podCount) // key: "namespace/podName"
	for i := range metrics.Pods {
		pod := &metrics.Pods[i]
		weight := calculatePodWeight(pod.CPUUsage, pod.MemUsage, totalCPU, totalMem, podCount)
		podCost := weight * totalAWSCost
		key := pod.Namespace + "/" + pod.Name
		podCosts[key] = podCost
	}

	// Build namespace aggregation.
	byNamespace := make(map[string]*NamespaceCost)
	byTeam := make(map[string]*TeamCost)

	for i := range metrics.Pods {
		pod := &metrics.Pods[i]
		podKey := pod.Namespace + "/" + pod.Name
		podCost := podCosts[podKey]

		// Get or create namespace cost entry.
		nsCost, ok := byNamespace[pod.Namespace]
		if !ok {
			nsCost = &NamespaceCost{
				Namespace:     pod.Namespace,
				ByService:     make(map[string]float64),
				ByPod:         make(map[string]float64),
				IsApproximate: costs.IsApproximate,
			}
			byNamespace[pod.Namespace] = nsCost
		}

		// Add pod cost to namespace.
		nsCost.DirectCost += podCost
		nsCost.ByPod[pod.Name] = podCost

		// Determine service label (try "app" first, then "service", default "unknown").
		service := podLabelOrDefault(pod.Labels, "unknown", "app", "service")
		nsCost.ByService[service] += podCost

		// Determine team label (try "team", default "unknown").
		team := podLabelOrDefault(pod.Labels, "unknown", "team")
		// Set namespace team to the first team seen; if multiple teams share
		// a namespace, the team with the most cost wins (resolved below).
		if nsCost.Team == "" {
			nsCost.Team = team
		}

		// Aggregate by team.
		tc, ok := byTeam[team]
		if !ok {
			tc = &TeamCost{
				Team:       team,
				Namespaces: make(map[string]float64),
			}
			byTeam[team] = tc
		}
		tc.DirectCost += podCost
		tc.Namespaces[pod.Namespace] += podCost
	}

	// Finalize namespace TotalCost (DirectCost only at this stage; SharedCost
	// is added later by AllocateSharedCosts).
	var totalCost float64
	for _, nsCost := range byNamespace {
		nsCost.TotalCost = nsCost.DirectCost
		totalCost += nsCost.TotalCost
	}

	// Finalize team TotalCost.
	for _, tc := range byTeam {
		tc.TotalCost = tc.DirectCost
	}

	return &AggregatedCosts{
		Timestamp:   time.Now().UTC(),
		ByNamespace: byNamespace,
		ByTeam:      byTeam,
		TotalCost:   totalCost,
	}, nil
}

// calculatePodWeight computes the proportional weight of a pod based on its
// CPU and memory usage relative to the cluster totals.
//
// Formula: (podCPU / totalCPU) * 0.5 + (podMem / totalMem) * 0.5
//
// Edge cases:
//   - If both totalCPU and totalMem are zero, distribute evenly (1/podCount).
//   - If only totalCPU is zero, use memory weight only.
//   - If only totalMem is zero, use CPU weight only.
func calculatePodWeight(podCPU, podMem, totalCPU, totalMem float64, podCount int) float64 {
	if podCount == 0 {
		return 0
	}

	cpuZero := totalCPU == 0
	memZero := totalMem == 0

	if cpuZero && memZero {
		// No resource usage at all — distribute evenly.
		return 1.0 / float64(podCount)
	}

	var cpuWeight, memWeight float64
	if cpuZero {
		// Only memory contributes; give it full weight.
		memWeight = podMem / totalMem
		return memWeight
	}
	if memZero {
		// Only CPU contributes; give it full weight.
		cpuWeight = podCPU / totalCPU
		return cpuWeight
	}

	// Both are non-zero — use 50/50 weighting.
	cpuWeight = podCPU / totalCPU
	memWeight = podMem / totalMem
	return cpuWeight*0.5 + memWeight*0.5
}

// podLabelOrDefault returns the value of the first matching label key found
// on the pod. If no keys match or labels is nil, it returns the default value.
func podLabelOrDefault(labels map[string]string, defaultVal string, keys ...string) string {
	if labels == nil {
		return defaultVal
	}
	for _, key := range keys {
		if val, ok := labels[key]; ok && val != "" {
			return val
		}
	}
	return defaultVal
}
