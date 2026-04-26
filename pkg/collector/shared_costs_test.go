package collector

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/go-logr/logr"
	metricsfake "k8s.io/metrics/pkg/client/clientset/versioned/fake"
	"k8s.io/client-go/kubernetes/fake"
)

// --- isSystemNamespace tests ---

func TestIsSystemNamespace(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		labels    map[string]string
		expected  bool
	}{
		{
			name:      "kube-system is system",
			namespace: "kube-system",
			expected:  true,
		},
		{
			name:      "kube-public is system",
			namespace: "kube-public",
			expected:  true,
		},
		{
			name:      "kube-node-lease is system",
			namespace: "kube-node-lease",
			expected:  true,
		},
		{
			name:      "regular namespace is not system",
			namespace: "production",
			expected:  false,
		},
		{
			name:      "namespace with shared label is system",
			namespace: "monitoring",
			labels:    map[string]string{"finops.eks.io/shared": "true"},
			expected:  true,
		},
		{
			name:      "namespace with shared label false is not system",
			namespace: "monitoring",
			labels:    map[string]string{"finops.eks.io/shared": "false"},
			expected:  false,
		},
		{
			name:      "nil labels only checks name",
			namespace: "default",
			labels:    nil,
			expected:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isSystemNamespace(tt.namespace, tt.labels)
			if got != tt.expected {
				t.Errorf("isSystemNamespace(%q, %v) = %v, want %v",
					tt.namespace, tt.labels, got, tt.expected)
			}
		})
	}
}

// --- allocateSharedCosts tests ---

func TestAllocateSharedCosts_NilInput(t *testing.T) {
	result := allocateSharedCosts(nil)
	if result != nil {
		t.Error("expected nil result for nil input")
	}
}

func TestAllocateSharedCosts_EmptyNamespaces(t *testing.T) {
	costs := &AggregatedCosts{
		Timestamp:   time.Now(),
		ByNamespace: map[string]*NamespaceCost{},
		ByTeam:      map[string]*TeamCost{},
		TotalCost:   0,
	}

	result := allocateSharedCosts(costs)
	if len(result.ByNamespace) != 0 {
		t.Errorf("expected 0 namespaces, got %d", len(result.ByNamespace))
	}
}

func TestAllocateSharedCosts_NoSharedCosts(t *testing.T) {
	costs := &AggregatedCosts{
		Timestamp: time.Now(),
		ByNamespace: map[string]*NamespaceCost{
			"app-ns": {
				Namespace:  "app-ns",
				Team:       "backend",
				DirectCost: 100.0,
				TotalCost:  100.0,
				ByService:  map[string]float64{"web": 100.0},
				ByPod:      map[string]float64{"pod-1": 100.0},
			},
		},
		ByTeam: map[string]*TeamCost{
			"backend": {
				Team:       "backend",
				DirectCost: 100.0,
				TotalCost:  100.0,
				Namespaces: map[string]float64{"app-ns": 100.0},
			},
		},
		TotalCost: 100.0,
	}

	result := allocateSharedCosts(costs)

	ns := result.ByNamespace["app-ns"]
	if ns.SharedCost != 0 {
		t.Errorf("expected no shared cost, got %f", ns.SharedCost)
	}
	if !floatEq(ns.TotalCost, 100.0) {
		t.Errorf("expected total cost 100.0, got %f", ns.TotalCost)
	}
}

func TestAllocateSharedCosts_NoNonSystemNamespaces(t *testing.T) {
	// All namespaces are system namespaces — shared costs remain unallocated.
	costs := &AggregatedCosts{
		Timestamp: time.Now(),
		ByNamespace: map[string]*NamespaceCost{
			"kube-system": {
				Namespace:  "kube-system",
				DirectCost: 50.0,
				TotalCost:  50.0,
				ByService:  map[string]float64{},
				ByPod:      map[string]float64{},
			},
			"kube-public": {
				Namespace:  "kube-public",
				DirectCost: 10.0,
				TotalCost:  10.0,
				ByService:  map[string]float64{},
				ByPod:      map[string]float64{},
			},
		},
		ByTeam:    map[string]*TeamCost{},
		TotalCost: 60.0,
	}

	result := allocateSharedCosts(costs)

	// System namespaces should remain unchanged.
	ks := result.ByNamespace["kube-system"]
	if ks.SharedCost != 0 {
		t.Errorf("expected kube-system shared cost 0, got %f", ks.SharedCost)
	}
	if !floatEq(ks.DirectCost, 50.0) {
		t.Errorf("expected kube-system direct cost 50.0, got %f", ks.DirectCost)
	}
}

func TestAllocateSharedCosts_ProportionalDistribution(t *testing.T) {
	costs := &AggregatedCosts{
		Timestamp: time.Now(),
		ByNamespace: map[string]*NamespaceCost{
			"kube-system": {
				Namespace:  "kube-system",
				DirectCost: 100.0,
				TotalCost:  100.0,
				ByService:  map[string]float64{},
				ByPod:      map[string]float64{},
			},
			"app-a": {
				Namespace:  "app-a",
				Team:       "team-a",
				DirectCost: 300.0,
				TotalCost:  300.0,
				ByService:  map[string]float64{"web": 300.0},
				ByPod:      map[string]float64{"pod-1": 300.0},
			},
			"app-b": {
				Namespace:  "app-b",
				Team:       "team-b",
				DirectCost: 100.0,
				TotalCost:  100.0,
				ByService:  map[string]float64{"api": 100.0},
				ByPod:      map[string]float64{"pod-2": 100.0},
			},
		},
		ByTeam: map[string]*TeamCost{
			"team-a": {Team: "team-a", DirectCost: 300.0, TotalCost: 300.0, Namespaces: map[string]float64{"app-a": 300.0}},
			"team-b": {Team: "team-b", DirectCost: 100.0, TotalCost: 100.0, Namespaces: map[string]float64{"app-b": 100.0}},
		},
		TotalCost: 500.0,
	}

	result := allocateSharedCosts(costs)

	// Total shared cost = 100.0 (from kube-system).
	// app-a has 300/400 = 75% → shared = 75.0
	// app-b has 100/400 = 25% → shared = 25.0
	appA := result.ByNamespace["app-a"]
	appB := result.ByNamespace["app-b"]

	if !floatEq(appA.SharedCost, 75.0) {
		t.Errorf("expected app-a shared cost 75.0, got %f", appA.SharedCost)
	}
	if !floatEq(appA.TotalCost, 375.0) {
		t.Errorf("expected app-a total cost 375.0, got %f", appA.TotalCost)
	}

	if !floatEq(appB.SharedCost, 25.0) {
		t.Errorf("expected app-b shared cost 25.0, got %f", appB.SharedCost)
	}
	if !floatEq(appB.TotalCost, 125.0) {
		t.Errorf("expected app-b total cost 125.0, got %f", appB.TotalCost)
	}

	// Sum of allocated shared costs should equal total shared cost.
	totalAllocated := appA.SharedCost + appB.SharedCost
	if !floatEq(totalAllocated, 100.0) {
		t.Errorf("sum of shared costs (%f) != total shared cost (100.0)", totalAllocated)
	}

	// Verify team aggregation includes indirect costs.
	teamA := result.ByTeam["team-a"]
	if !floatEq(teamA.IndirectCost, 75.0) {
		t.Errorf("expected team-a indirect cost 75.0, got %f", teamA.IndirectCost)
	}
	if !floatEq(teamA.TotalCost, 375.0) {
		t.Errorf("expected team-a total cost 375.0, got %f", teamA.TotalCost)
	}

	teamB := result.ByTeam["team-b"]
	if !floatEq(teamB.IndirectCost, 25.0) {
		t.Errorf("expected team-b indirect cost 25.0, got %f", teamB.IndirectCost)
	}
	if !floatEq(teamB.TotalCost, 125.0) {
		t.Errorf("expected team-b total cost 125.0, got %f", teamB.TotalCost)
	}
}

func TestAllocateSharedCosts_ZeroDirectCostsDistributeEvenly(t *testing.T) {
	costs := &AggregatedCosts{
		Timestamp: time.Now(),
		ByNamespace: map[string]*NamespaceCost{
			"kube-system": {
				Namespace:  "kube-system",
				DirectCost: 90.0,
				TotalCost:  90.0,
				ByService:  map[string]float64{},
				ByPod:      map[string]float64{},
			},
			"app-a": {
				Namespace:  "app-a",
				Team:       "team-a",
				DirectCost: 0,
				TotalCost:  0,
				ByService:  map[string]float64{},
				ByPod:      map[string]float64{},
			},
			"app-b": {
				Namespace:  "app-b",
				Team:       "team-b",
				DirectCost: 0,
				TotalCost:  0,
				ByService:  map[string]float64{},
				ByPod:      map[string]float64{},
			},
			"app-c": {
				Namespace:  "app-c",
				Team:       "team-c",
				DirectCost: 0,
				TotalCost:  0,
				ByService:  map[string]float64{},
				ByPod:      map[string]float64{},
			},
		},
		ByTeam:    map[string]*TeamCost{},
		TotalCost: 90.0,
	}

	result := allocateSharedCosts(costs)

	// 90.0 shared cost / 3 non-system namespaces = 30.0 each.
	for _, nsName := range []string{"app-a", "app-b", "app-c"} {
		ns := result.ByNamespace[nsName]
		if !floatEq(ns.SharedCost, 30.0) {
			t.Errorf("expected %s shared cost 30.0, got %f", nsName, ns.SharedCost)
		}
		if !floatEq(ns.TotalCost, 30.0) {
			t.Errorf("expected %s total cost 30.0, got %f", nsName, ns.TotalCost)
		}
	}
}

func TestAllocateSharedCosts_UnallocatedTeam(t *testing.T) {
	// Namespace without a team label should go to "unallocated".
	costs := &AggregatedCosts{
		Timestamp: time.Now(),
		ByNamespace: map[string]*NamespaceCost{
			"kube-system": {
				Namespace:  "kube-system",
				DirectCost: 50.0,
				TotalCost:  50.0,
				ByService:  map[string]float64{},
				ByPod:      map[string]float64{},
			},
			"orphan-ns": {
				Namespace:  "orphan-ns",
				Team:       "", // no team label
				DirectCost: 100.0,
				TotalCost:  100.0,
				ByService:  map[string]float64{},
				ByPod:      map[string]float64{},
			},
		},
		ByTeam:    map[string]*TeamCost{},
		TotalCost: 150.0,
	}

	result := allocateSharedCosts(costs)

	// orphan-ns gets all 50.0 shared cost (only non-system namespace).
	orphan := result.ByNamespace["orphan-ns"]
	if !floatEq(orphan.SharedCost, 50.0) {
		t.Errorf("expected orphan-ns shared cost 50.0, got %f", orphan.SharedCost)
	}

	// Team aggregation should have "unallocated" team.
	unalloc := result.ByTeam[UnallocatedTeam]
	if unalloc == nil {
		t.Fatal("expected 'unallocated' team in ByTeam")
	}
	if !floatEq(unalloc.IndirectCost, 50.0) {
		t.Errorf("expected unallocated indirect cost 50.0, got %f", unalloc.IndirectCost)
	}
	if !floatEq(unalloc.DirectCost, 150.0) {
		// 100.0 from orphan-ns + 50.0 from kube-system (also no team)
		t.Errorf("expected unallocated direct cost 150.0, got %f", unalloc.DirectCost)
	}
}

func TestAllocateSharedCosts_MultipleSystemNamespaces(t *testing.T) {
	costs := &AggregatedCosts{
		Timestamp: time.Now(),
		ByNamespace: map[string]*NamespaceCost{
			"kube-system": {
				Namespace:  "kube-system",
				DirectCost: 40.0,
				TotalCost:  40.0,
				ByService:  map[string]float64{},
				ByPod:      map[string]float64{},
			},
			"kube-public": {
				Namespace:  "kube-public",
				DirectCost: 10.0,
				TotalCost:  10.0,
				ByService:  map[string]float64{},
				ByPod:      map[string]float64{},
			},
			"kube-node-lease": {
				Namespace:  "kube-node-lease",
				DirectCost: 5.0,
				TotalCost:  5.0,
				ByService:  map[string]float64{},
				ByPod:      map[string]float64{},
			},
			"production": {
				Namespace:  "production",
				Team:       "platform",
				DirectCost: 200.0,
				TotalCost:  200.0,
				ByService:  map[string]float64{"api": 200.0},
				ByPod:      map[string]float64{"pod-1": 200.0},
			},
		},
		ByTeam:    map[string]*TeamCost{},
		TotalCost: 255.0,
	}

	result := allocateSharedCosts(costs)

	// Total shared = 40 + 10 + 5 = 55.0
	// Only one non-system namespace → gets all 55.0
	prod := result.ByNamespace["production"]
	if !floatEq(prod.SharedCost, 55.0) {
		t.Errorf("expected production shared cost 55.0, got %f", prod.SharedCost)
	}
	if !floatEq(prod.TotalCost, 255.0) {
		t.Errorf("expected production total cost 255.0, got %f", prod.TotalCost)
	}
}

func TestAllocateSharedCosts_SystemNamespaceWithZeroCost(t *testing.T) {
	// System namespace exists but has zero cost — nothing to distribute.
	costs := &AggregatedCosts{
		Timestamp: time.Now(),
		ByNamespace: map[string]*NamespaceCost{
			"kube-system": {
				Namespace:  "kube-system",
				DirectCost: 0,
				TotalCost:  0,
				ByService:  map[string]float64{},
				ByPod:      map[string]float64{},
			},
			"app-ns": {
				Namespace:  "app-ns",
				Team:       "dev",
				DirectCost: 100.0,
				TotalCost:  100.0,
				ByService:  map[string]float64{},
				ByPod:      map[string]float64{},
			},
		},
		ByTeam:    map[string]*TeamCost{},
		TotalCost: 100.0,
	}

	result := allocateSharedCosts(costs)

	appNS := result.ByNamespace["app-ns"]
	if appNS.SharedCost != 0 {
		t.Errorf("expected no shared cost when system namespace has zero cost, got %f", appNS.SharedCost)
	}
}

// --- DefaultCostCollector.AllocateSharedCosts integration test ---

func TestDefaultCostCollector_AllocateSharedCosts(t *testing.T) {
	ctx := context.Background()
	kubeClient := fake.NewSimpleClientset()
	metricsClientset := metricsfake.NewSimpleClientset()

	collector := NewDefaultCostCollector(
		kubeClient,
		metricsClientset.MetricsV1beta1(),
		logr.Discard(),
	)

	costs := &AggregatedCosts{
		Timestamp: time.Now(),
		ByNamespace: map[string]*NamespaceCost{
			"kube-system": {
				Namespace:  "kube-system",
				DirectCost: 50.0,
				TotalCost:  50.0,
				ByService:  map[string]float64{},
				ByPod:      map[string]float64{},
			},
			"app": {
				Namespace:  "app",
				Team:       "eng",
				DirectCost: 200.0,
				TotalCost:  200.0,
				ByService:  map[string]float64{"web": 200.0},
				ByPod:      map[string]float64{"pod-1": 200.0},
			},
		},
		ByTeam:    map[string]*TeamCost{},
		TotalCost: 250.0,
	}

	result, err := collector.AllocateSharedCosts(ctx, costs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	appNS := result.ByNamespace["app"]
	if !floatEq(appNS.SharedCost, 50.0) {
		t.Errorf("expected shared cost 50.0, got %f", appNS.SharedCost)
	}
	if !floatEq(appNS.TotalCost, 250.0) {
		t.Errorf("expected total cost 250.0, got %f", appNS.TotalCost)
	}
}

func TestDefaultCostCollector_AllocateSharedCosts_NilInput(t *testing.T) {
	ctx := context.Background()
	kubeClient := fake.NewSimpleClientset()
	metricsClientset := metricsfake.NewSimpleClientset()

	collector := NewDefaultCostCollector(
		kubeClient,
		metricsClientset.MetricsV1beta1(),
		logr.Discard(),
	)

	_, err := collector.AllocateSharedCosts(ctx, nil)
	if err == nil {
		t.Error("expected error for nil input")
	}
}

// floatEq compares two float64 values with a small tolerance.
func floatEq(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}
