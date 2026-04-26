package instance

import (
	"context"
	"testing"
	"time"

	"github.com/go-logr/logr"
	v1alpha1 "github.com/eks-financial-orchestrator/api/v1alpha1"
	"github.com/eks-financial-orchestrator/pkg/audit"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

// --- Test helpers ---

// fakeAuditWriter records audit entries for assertions.
type fakeAuditWriter struct {
	entries []*audit.AuditLogEntry
}

func (w *fakeAuditWriter) WriteEntry(_ context.Context, entry *audit.AuditLogEntry) error {
	w.entries = append(w.entries, entry)
	return nil
}

func (w *fakeAuditWriter) QueryEntries(_ context.Context, _ audit.AuditQueryFilters) ([]audit.AuditLogEntry, error) {
	return nil, nil
}

// fakeCostReader returns canned daily cost records.
type fakeCostReader struct {
	records []DailyCostRecord
}

func (r *fakeCostReader) GetDailyCosts(_ context.Context, _, _ time.Time) ([]DailyCostRecord, error) {
	return r.records, nil
}

// newTestManager creates a DefaultInstanceManager with a fake Kubernetes
// client seeded with the given nodes.
func newTestManager(nodes []corev1.Node, costRecords []DailyCostRecord) (*DefaultInstanceManager, *fakeAuditWriter) {
	client := fake.NewSimpleClientset()
	for i := range nodes {
		_, _ = client.CoreV1().Nodes().Create(context.Background(), &nodes[i], metav1.CreateOptions{})
	}
	aw := &fakeAuditWriter{}
	cr := &fakeCostReader{records: costRecords}
	mgr := NewDefaultInstanceManager(client, logr.Discard(), cr, aw)
	return mgr, aw
}

func makeNode(name string, labels map[string]string) corev1.Node {
	return corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
	}
}

// --- AnalyzeInstanceMix tests ---

func TestAnalyzeInstanceMix_MixedNodeTypes(t *testing.T) {
	nodes := []corev1.Node{
		makeNode("node-1", map[string]string{"eks.amazonaws.com/capacityType": "ON_DEMAND"}),
		makeNode("node-2", map[string]string{"eks.amazonaws.com/capacityType": "ON_DEMAND"}),
		makeNode("node-3", map[string]string{"karpenter.sh/capacity-type": "spot"}),
		makeNode("node-4", map[string]string{"finops.eks.io/purchase-option": "reserved"}),
		makeNode("node-5", map[string]string{"karpenter.sh/capacity-type": "spot"}),
	}

	mgr, _ := newTestManager(nodes, nil)
	analysis, err := mgr.AnalyzeInstanceMix(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if analysis.TotalNodes != 5 {
		t.Errorf("expected 5 total nodes, got %d", analysis.TotalNodes)
	}

	// Verify counts per purchase option.
	assertOptionCount(t, analysis, "on_demand", 2)
	assertOptionCount(t, analysis, "spot", 2)
	assertOptionCount(t, analysis, "reserved", 1)

	// Verify percentages.
	assertOptionPct(t, analysis, "on_demand", 40.0)
	assertOptionPct(t, analysis, "spot", 40.0)
	assertOptionPct(t, analysis, "reserved", 20.0)

	// Current cost should be positive.
	if analysis.CurrentCost <= 0 {
		t.Errorf("expected positive current cost, got %f", analysis.CurrentCost)
	}

	// Potential savings should be non-negative.
	if analysis.PotentialSavings < 0 {
		t.Errorf("expected non-negative potential savings, got %f", analysis.PotentialSavings)
	}
}

func TestAnalyzeInstanceMix_NoLabels_DefaultsToOnDemand(t *testing.T) {
	nodes := []corev1.Node{
		makeNode("node-1", nil),
		makeNode("node-2", map[string]string{"some-other-label": "value"}),
	}

	mgr, _ := newTestManager(nodes, nil)
	analysis, err := mgr.AnalyzeInstanceMix(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertOptionCount(t, analysis, "on_demand", 2)
}

func assertOptionCount(t *testing.T, a *InstanceMixAnalysis, option string, expected int) {
	t.Helper()
	stats, ok := a.ByPurchaseOption[option]
	if !ok {
		if expected == 0 {
			return
		}
		t.Errorf("expected purchase option %q in analysis", option)
		return
	}
	if stats.Count != expected {
		t.Errorf("option %q: expected count %d, got %d", option, expected, stats.Count)
	}
}

func assertOptionPct(t *testing.T, a *InstanceMixAnalysis, option string, expected float64) {
	t.Helper()
	stats, ok := a.ByPurchaseOption[option]
	if !ok {
		t.Errorf("expected purchase option %q in analysis", option)
		return
	}
	if diff := stats.Percentage - expected; diff > 0.01 || diff < -0.01 {
		t.Errorf("option %q: expected percentage %.1f, got %.1f", option, expected, stats.Percentage)
	}
}

// --- GenerateRecommendation tests ---

func TestGenerateRecommendation_RespectsMinOnDemand(t *testing.T) {
	analysis := &InstanceMixAnalysis{
		Timestamp:  time.Now().UTC(),
		TotalNodes: 10,
		ByPurchaseOption: map[string]*PurchaseOptionStats{
			"on_demand": {Count: 8, Percentage: 80.0, Cost: 8 * 0.192 * 730},
			"spot":      {Count: 2, Percentage: 20.0, Cost: 2 * 0.070 * 730},
		},
		CurrentCost: 8*0.192*730 + 2*0.070*730,
	}

	policies := []*v1alpha1.FinancialPolicy{
		{
			Spec: v1alpha1.FinancialPolicySpec{
				InstanceMix: &v1alpha1.InstanceMixSpec{
					MinOnDemandPercent: 30.0,
					MaxSpotPercent:     60.0,
				},
			},
		},
	}

	mgr, _ := newTestManager(nil, nil)
	rec, err := mgr.GenerateRecommendation(context.Background(), analysis, policies)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// On-Demand must be >= 30%.
	if rec.RecommendedMix["on_demand"] < 30.0 {
		t.Errorf("expected on_demand >= 30%%, got %.1f%%", rec.RecommendedMix["on_demand"])
	}

	// Spot must be <= 60%.
	if rec.RecommendedMix["spot"] > 60.0 {
		t.Errorf("expected spot <= 60%%, got %.1f%%", rec.RecommendedMix["spot"])
	}

	// Expected savings must be non-negative.
	if rec.ExpectedSavings < 0 {
		t.Errorf("expected non-negative savings, got %f", rec.ExpectedSavings)
	}

	if !rec.PolicyCompliant {
		t.Error("expected recommendation to be policy compliant")
	}
}

func TestGenerateRecommendation_HighMinOnDemand(t *testing.T) {
	analysis := &InstanceMixAnalysis{
		Timestamp:  time.Now().UTC(),
		TotalNodes: 4,
		ByPurchaseOption: map[string]*PurchaseOptionStats{
			"on_demand": {Count: 1, Percentage: 25.0, Cost: 1 * 0.192 * 730},
			"spot":      {Count: 3, Percentage: 75.0, Cost: 3 * 0.070 * 730},
		},
		CurrentCost: 1*0.192*730 + 3*0.070*730,
	}

	// Policy requires 80% On-Demand minimum.
	policies := []*v1alpha1.FinancialPolicy{
		{
			Spec: v1alpha1.FinancialPolicySpec{
				InstanceMix: &v1alpha1.InstanceMixSpec{
					MinOnDemandPercent: 80.0,
					MaxSpotPercent:     20.0,
				},
			},
		},
	}

	mgr, _ := newTestManager(nil, nil)
	rec, err := mgr.GenerateRecommendation(context.Background(), analysis, policies)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if rec.RecommendedMix["on_demand"] < 80.0 {
		t.Errorf("expected on_demand >= 80%%, got %.1f%%", rec.RecommendedMix["on_demand"])
	}

	if rec.RecommendedMix["spot"] > 20.0 {
		t.Errorf("expected spot <= 20%%, got %.1f%%", rec.RecommendedMix["spot"])
	}

	if rec.ExpectedSavings < 0 {
		t.Errorf("expected non-negative savings, got %f", rec.ExpectedSavings)
	}
}

func TestGenerateRecommendation_NoPolicies(t *testing.T) {
	analysis := &InstanceMixAnalysis{
		Timestamp:  time.Now().UTC(),
		TotalNodes: 5,
		ByPurchaseOption: map[string]*PurchaseOptionStats{
			"on_demand": {Count: 5, Percentage: 100.0, Cost: 5 * 0.192 * 730},
		},
		CurrentCost: 5 * 0.192 * 730,
	}

	mgr, _ := newTestManager(nil, nil)
	rec, err := mgr.GenerateRecommendation(context.Background(), analysis, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// With no policies, Spot should be maximized (up to 100%).
	if rec.RecommendedMix["spot"] <= 0 {
		t.Errorf("expected some Spot in recommendation, got %.1f%%", rec.RecommendedMix["spot"])
	}

	if rec.ExpectedSavings < 0 {
		t.Errorf("expected non-negative savings, got %f", rec.ExpectedSavings)
	}
}

// --- ApplyRecommendation tests ---

func TestApplyRecommendation_RecordsAudit(t *testing.T) {
	mgr, aw := newTestManager(nil, nil)

	rec := &OptimizationRecommendation{
		ID:              "rec-test-1",
		Timestamp:       time.Now().UTC(),
		CurrentMix:      map[string]float64{"on_demand": 80, "spot": 20},
		RecommendedMix:  map[string]float64{"on_demand": 40, "spot": 60},
		ExpectedSavings: 500.0,
		Reason:          "test optimization",
		AffectedNodes:   []string{"node-1", "node-2"},
		PolicyCompliant: true,
	}

	result, err := mgr.ApplyRecommendation(context.Background(), rec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Applied {
		t.Error("expected recommendation to be applied")
	}
	if result.RecommendationID != "rec-test-1" {
		t.Errorf("expected recommendation ID rec-test-1, got %s", result.RecommendationID)
	}

	// Verify audit entry was written.
	if len(aw.entries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(aw.entries))
	}
	entry := aw.entries[0]
	if entry.Action != "optimization_apply" {
		t.Errorf("expected action optimization_apply, got %s", entry.Action)
	}
	if entry.ExpectedSavings != 500.0 {
		t.Errorf("expected savings 500.0, got %f", entry.ExpectedSavings)
	}
	if entry.Result != "success" {
		t.Errorf("expected result success, got %s", entry.Result)
	}
}

// --- HandleSpotInterruption tests ---

func TestHandleSpotInterruption_BasicFlow(t *testing.T) {
	client := fake.NewSimpleClientset()

	// Create a node and pods on it.
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "spot-node-1"},
	}
	_, _ = client.CoreV1().Nodes().Create(context.Background(), node, metav1.CreateOptions{})

	pod1 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pod-1", Namespace: "default"},
		Spec:       corev1.PodSpec{NodeName: "spot-node-1"},
	}
	pod2 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pod-2", Namespace: "kube-system"},
		Spec:       corev1.PodSpec{NodeName: "spot-node-1"},
	}
	_, _ = client.CoreV1().Pods("default").Create(context.Background(), pod1, metav1.CreateOptions{})
	_, _ = client.CoreV1().Pods("kube-system").Create(context.Background(), pod2, metav1.CreateOptions{})

	// Track eviction calls.
	evictionCount := 0
	client.PrependReactor("create", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if action.GetSubresource() == "eviction" {
			evictionCount++
			return true, nil, nil
		}
		return false, nil, nil
	})

	mgr := NewDefaultInstanceManager(client, logr.Discard(), nil, nil)
	err := mgr.HandleSpotInterruption(context.Background(), "spot-node-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if evictionCount != 2 {
		t.Errorf("expected 2 evictions, got %d", evictionCount)
	}
}

func TestHandleSpotInterruption_NoPods(t *testing.T) {
	client := fake.NewSimpleClientset()
	mgr := NewDefaultInstanceManager(client, logr.Discard(), nil, nil)

	err := mgr.HandleSpotInterruption(context.Background(), "empty-node")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- EvaluateSavingsPlans tests ---

func TestEvaluateSavingsPlans_NonNegativeSavings(t *testing.T) {
	now := time.Now().UTC()
	records := make([]DailyCostRecord, 30)
	for i := 0; i < 30; i++ {
		records[i] = DailyCostRecord{
			Date:      now.AddDate(0, 0, -30+i),
			TotalCost: 100.0 + float64(i)*2.0, // increasing daily cost
		}
	}

	mgr, _ := newTestManager(nil, records)
	rec, err := mgr.EvaluateSavingsPlans(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if rec.EstimatedSavings < 0 {
		t.Errorf("expected non-negative savings, got %f", rec.EstimatedSavings)
	}
	if rec.AnalysisPeriodDays != 30 {
		t.Errorf("expected 30-day analysis period, got %d", rec.AnalysisPeriodDays)
	}
	if rec.CurrentMonthlyCost <= 0 {
		t.Errorf("expected positive monthly cost, got %f", rec.CurrentMonthlyCost)
	}
	if rec.RecommendedPlan == "" {
		t.Error("expected a recommended plan")
	}
	if rec.CommitmentAmount <= 0 {
		t.Errorf("expected positive commitment amount, got %f", rec.CommitmentAmount)
	}
	if rec.CoveragePercent <= 0 {
		t.Errorf("expected positive coverage percent, got %f", rec.CoveragePercent)
	}
}

func TestEvaluateSavingsPlans_NoHistoricalData(t *testing.T) {
	mgr, _ := newTestManager(nil, nil)
	rec, err := mgr.EvaluateSavingsPlans(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if rec.EstimatedSavings != 0 {
		t.Errorf("expected zero savings with no data, got %f", rec.EstimatedSavings)
	}
	if rec.CurrentMonthlyCost != 0 {
		t.Errorf("expected zero monthly cost with no data, got %f", rec.CurrentMonthlyCost)
	}
}

func TestEvaluateSavingsPlans_ZeroCosts(t *testing.T) {
	now := time.Now().UTC()
	records := make([]DailyCostRecord, 10)
	for i := 0; i < 10; i++ {
		records[i] = DailyCostRecord{
			Date:      now.AddDate(0, 0, -10+i),
			TotalCost: 0.0,
		}
	}

	mgr, _ := newTestManager(nil, records)
	rec, err := mgr.EvaluateSavingsPlans(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if rec.EstimatedSavings < 0 {
		t.Errorf("expected non-negative savings, got %f", rec.EstimatedSavings)
	}
}
