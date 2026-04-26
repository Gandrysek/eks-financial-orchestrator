package collector

import (
	"context"
	"math"
	"testing"
)

func TestCorrelateAndAggregate_BasicDistribution(t *testing.T) {
	ctx := context.Background()

	metrics := &ClusterMetrics{
		Pods: []PodMetrics{
			{
				Name:      "pod-a",
				Namespace: "ns1",
				CPUUsage:  1.0,
				MemUsage:  1000.0,
				Labels:    map[string]string{"app": "web", "team": "platform"},
			},
			{
				Name:      "pod-b",
				Namespace: "ns1",
				CPUUsage:  1.0,
				MemUsage:  1000.0,
				Labels:    map[string]string{"app": "web", "team": "platform"},
			},
		},
	}

	costs := &AWSCostData{
		TotalCost: 100.0,
	}

	result, err := correlateAndAggregate(ctx, metrics, costs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Two identical pods should each get 50% of the cost.
	if len(result.ByNamespace) != 1 {
		t.Fatalf("expected 1 namespace, got %d", len(result.ByNamespace))
	}

	ns := result.ByNamespace["ns1"]
	if ns == nil {
		t.Fatal("expected namespace ns1")
	}

	if !floatEqual(ns.DirectCost, 100.0) {
		t.Errorf("expected namespace direct cost 100.0, got %f", ns.DirectCost)
	}

	if !floatEqual(ns.ByPod["pod-a"], 50.0) {
		t.Errorf("expected pod-a cost 50.0, got %f", ns.ByPod["pod-a"])
	}
	if !floatEqual(ns.ByPod["pod-b"], 50.0) {
		t.Errorf("expected pod-b cost 50.0, got %f", ns.ByPod["pod-b"])
	}

	// Total cost should equal sum of namespace costs.
	if !floatEqual(result.TotalCost, 100.0) {
		t.Errorf("expected total cost 100.0, got %f", result.TotalCost)
	}
}

func TestCorrelateAndAggregate_MultipleNamespaces(t *testing.T) {
	ctx := context.Background()

	metrics := &ClusterMetrics{
		Pods: []PodMetrics{
			{
				Name:      "pod-a",
				Namespace: "ns1",
				CPUUsage:  2.0,
				MemUsage:  2000.0,
				Labels:    map[string]string{"app": "api", "team": "backend"},
			},
			{
				Name:      "pod-b",
				Namespace: "ns2",
				CPUUsage:  2.0,
				MemUsage:  2000.0,
				Labels:    map[string]string{"service": "worker", "team": "data"},
			},
		},
	}

	costs := &AWSCostData{
		TotalCost: 200.0,
	}

	result, err := correlateAndAggregate(ctx, metrics, costs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.ByNamespace) != 2 {
		t.Fatalf("expected 2 namespaces, got %d", len(result.ByNamespace))
	}

	// Equal resource usage → equal cost split.
	ns1 := result.ByNamespace["ns1"]
	ns2 := result.ByNamespace["ns2"]
	if !floatEqual(ns1.DirectCost, 100.0) {
		t.Errorf("expected ns1 cost 100.0, got %f", ns1.DirectCost)
	}
	if !floatEqual(ns2.DirectCost, 100.0) {
		t.Errorf("expected ns2 cost 100.0, got %f", ns2.DirectCost)
	}

	// Verify service labels: pod-a has "app" label, pod-b has "service" label.
	if ns1.ByService["api"] == 0 {
		t.Error("expected ns1 to have service 'api'")
	}
	if ns2.ByService["worker"] == 0 {
		t.Error("expected ns2 to have service 'worker'")
	}

	// Verify team aggregation.
	if len(result.ByTeam) != 2 {
		t.Fatalf("expected 2 teams, got %d", len(result.ByTeam))
	}
	if !floatEqual(result.ByTeam["backend"].DirectCost, 100.0) {
		t.Errorf("expected backend team cost 100.0, got %f", result.ByTeam["backend"].DirectCost)
	}
	if !floatEqual(result.ByTeam["data"].DirectCost, 100.0) {
		t.Errorf("expected data team cost 100.0, got %f", result.ByTeam["data"].DirectCost)
	}

	// Total cost invariant.
	if !floatEqual(result.TotalCost, 200.0) {
		t.Errorf("expected total cost 200.0, got %f", result.TotalCost)
	}
}

func TestCorrelateAndAggregate_ZeroResourceUsage(t *testing.T) {
	ctx := context.Background()

	// All pods have zero CPU and memory — should distribute evenly.
	metrics := &ClusterMetrics{
		Pods: []PodMetrics{
			{Name: "pod-a", Namespace: "ns1", CPUUsage: 0, MemUsage: 0},
			{Name: "pod-b", Namespace: "ns1", CPUUsage: 0, MemUsage: 0},
			{Name: "pod-c", Namespace: "ns2", CPUUsage: 0, MemUsage: 0},
		},
	}

	costs := &AWSCostData{TotalCost: 300.0}

	result, err := correlateAndAggregate(ctx, metrics, costs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Each pod gets 1/3 of the cost = 100.0.
	ns1 := result.ByNamespace["ns1"]
	ns2 := result.ByNamespace["ns2"]

	if !floatEqual(ns1.DirectCost, 200.0) {
		t.Errorf("expected ns1 cost 200.0, got %f", ns1.DirectCost)
	}
	if !floatEqual(ns2.DirectCost, 100.0) {
		t.Errorf("expected ns2 cost 100.0, got %f", ns2.DirectCost)
	}
}

func TestCorrelateAndAggregate_MissingLabels(t *testing.T) {
	ctx := context.Background()

	metrics := &ClusterMetrics{
		Pods: []PodMetrics{
			{
				Name:      "pod-no-labels",
				Namespace: "ns1",
				CPUUsage:  1.0,
				MemUsage:  1000.0,
				Labels:    nil, // no labels at all
			},
		},
	}

	costs := &AWSCostData{TotalCost: 50.0}

	result, err := correlateAndAggregate(ctx, metrics, costs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ns := result.ByNamespace["ns1"]
	if ns.ByService["unknown"] == 0 {
		t.Error("expected pod without labels to be in 'unknown' service")
	}

	if result.ByTeam["unknown"] == nil {
		t.Error("expected pod without labels to be in 'unknown' team")
	}
}

func TestCorrelateAndAggregate_IsApproximatePropagation(t *testing.T) {
	ctx := context.Background()

	metrics := &ClusterMetrics{
		Pods: []PodMetrics{
			{Name: "pod-a", Namespace: "ns1", CPUUsage: 1.0, MemUsage: 1000.0},
		},
	}

	costs := &AWSCostData{
		TotalCost:     100.0,
		IsApproximate: true,
	}

	result, err := correlateAndAggregate(ctx, metrics, costs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ns := result.ByNamespace["ns1"]
	if !ns.IsApproximate {
		t.Error("expected IsApproximate to be propagated to namespace cost")
	}
}

func TestCorrelateAndAggregate_EmptyMetrics(t *testing.T) {
	ctx := context.Background()

	metrics := &ClusterMetrics{Pods: nil}
	costs := &AWSCostData{TotalCost: 100.0}

	result, err := correlateAndAggregate(ctx, metrics, costs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.ByNamespace) != 0 {
		t.Errorf("expected 0 namespaces, got %d", len(result.ByNamespace))
	}
	if result.TotalCost != 0 {
		t.Errorf("expected total cost 0, got %f", result.TotalCost)
	}
}

func TestCorrelateAndAggregate_NilInputs(t *testing.T) {
	ctx := context.Background()

	result, err := correlateAndAggregate(ctx, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if len(result.ByNamespace) != 0 {
		t.Errorf("expected 0 namespaces, got %d", len(result.ByNamespace))
	}
	if result.TotalCost != 0 {
		t.Errorf("expected total cost 0, got %f", result.TotalCost)
	}
}

func TestCorrelateAndAggregate_WeightedDistribution(t *testing.T) {
	ctx := context.Background()

	// pod-a uses 3x the CPU and memory of pod-b.
	metrics := &ClusterMetrics{
		Pods: []PodMetrics{
			{
				Name:      "pod-a",
				Namespace: "ns1",
				CPUUsage:  3.0,
				MemUsage:  3000.0,
				Labels:    map[string]string{"app": "heavy"},
			},
			{
				Name:      "pod-b",
				Namespace: "ns1",
				CPUUsage:  1.0,
				MemUsage:  1000.0,
				Labels:    map[string]string{"app": "light"},
			},
		},
	}

	costs := &AWSCostData{TotalCost: 100.0}

	result, err := correlateAndAggregate(ctx, metrics, costs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ns := result.ByNamespace["ns1"]
	// pod-a: (3/4)*0.5 + (3000/4000)*0.5 = 0.375 + 0.375 = 0.75 → $75
	// pod-b: (1/4)*0.5 + (1000/4000)*0.5 = 0.125 + 0.125 = 0.25 → $25
	if !floatEqual(ns.ByPod["pod-a"], 75.0) {
		t.Errorf("expected pod-a cost 75.0, got %f", ns.ByPod["pod-a"])
	}
	if !floatEqual(ns.ByPod["pod-b"], 25.0) {
		t.Errorf("expected pod-b cost 25.0, got %f", ns.ByPod["pod-b"])
	}
}

func TestCorrelateAndAggregate_EveryPodInExactlyOneNamespace(t *testing.T) {
	ctx := context.Background()

	metrics := &ClusterMetrics{
		Pods: []PodMetrics{
			{Name: "p1", Namespace: "ns1", CPUUsage: 1.0, MemUsage: 100.0},
			{Name: "p2", Namespace: "ns2", CPUUsage: 1.0, MemUsage: 100.0},
			{Name: "p3", Namespace: "ns1", CPUUsage: 1.0, MemUsage: 100.0},
			{Name: "p4", Namespace: "ns3", CPUUsage: 1.0, MemUsage: 100.0},
		},
	}

	costs := &AWSCostData{TotalCost: 400.0}

	result, err := correlateAndAggregate(ctx, metrics, costs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Count total pods across all namespaces.
	totalPods := 0
	for _, ns := range result.ByNamespace {
		totalPods += len(ns.ByPod)
	}
	if totalPods != 4 {
		t.Errorf("expected 4 total pods across namespaces, got %d", totalPods)
	}

	// Verify each pod appears in exactly one namespace.
	podNamespaces := make(map[string]string)
	for nsName, ns := range result.ByNamespace {
		for podName := range ns.ByPod {
			if existing, ok := podNamespaces[podName]; ok {
				t.Errorf("pod %s appears in both %s and %s", podName, existing, nsName)
			}
			podNamespaces[podName] = nsName
		}
	}

	// Verify sum of namespace costs equals total cost.
	var sumNS float64
	for _, ns := range result.ByNamespace {
		sumNS += ns.TotalCost
	}
	if !floatEqual(sumNS, result.TotalCost) {
		t.Errorf("sum of namespace costs (%f) != total cost (%f)", sumNS, result.TotalCost)
	}
}

func TestCalculatePodWeight(t *testing.T) {
	tests := []struct {
		name     string
		podCPU   float64
		podMem   float64
		totalCPU float64
		totalMem float64
		podCount int
		expected float64
	}{
		{
			name:     "equal share of two pods",
			podCPU:   1.0,
			podMem:   1000.0,
			totalCPU: 2.0,
			totalMem: 2000.0,
			podCount: 2,
			expected: 0.5,
		},
		{
			name:     "zero total CPU and memory",
			podCPU:   0,
			podMem:   0,
			totalCPU: 0,
			totalMem: 0,
			podCount: 4,
			expected: 0.25,
		},
		{
			name:     "zero total CPU only",
			podCPU:   0,
			podMem:   500.0,
			totalCPU: 0,
			totalMem: 1000.0,
			podCount: 2,
			expected: 0.5,
		},
		{
			name:     "zero total memory only",
			podCPU:   2.0,
			podMem:   0,
			totalCPU: 4.0,
			totalMem: 0,
			podCount: 2,
			expected: 0.5,
		},
		{
			name:     "zero pod count",
			podCPU:   1.0,
			podMem:   1000.0,
			totalCPU: 2.0,
			totalMem: 2000.0,
			podCount: 0,
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculatePodWeight(tt.podCPU, tt.podMem, tt.totalCPU, tt.totalMem, tt.podCount)
			if !floatEqual(got, tt.expected) {
				t.Errorf("calculatePodWeight() = %f, want %f", got, tt.expected)
			}
		})
	}
}

func TestPodLabelOrDefault(t *testing.T) {
	tests := []struct {
		name       string
		labels     map[string]string
		defaultVal string
		keys       []string
		expected   string
	}{
		{
			name:       "first key matches",
			labels:     map[string]string{"app": "web", "service": "api"},
			defaultVal: "unknown",
			keys:       []string{"app", "service"},
			expected:   "web",
		},
		{
			name:       "second key matches",
			labels:     map[string]string{"service": "api"},
			defaultVal: "unknown",
			keys:       []string{"app", "service"},
			expected:   "api",
		},
		{
			name:       "no keys match",
			labels:     map[string]string{"other": "value"},
			defaultVal: "unknown",
			keys:       []string{"app", "service"},
			expected:   "unknown",
		},
		{
			name:       "nil labels",
			labels:     nil,
			defaultVal: "default",
			keys:       []string{"app"},
			expected:   "default",
		},
		{
			name:       "empty value treated as missing",
			labels:     map[string]string{"app": ""},
			defaultVal: "unknown",
			keys:       []string{"app"},
			expected:   "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := podLabelOrDefault(tt.labels, tt.defaultVal, tt.keys...)
			if got != tt.expected {
				t.Errorf("podLabelOrDefault() = %s, want %s", got, tt.expected)
			}
		})
	}
}

// floatEqual compares two float64 values with a small tolerance.
func floatEqual(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}
