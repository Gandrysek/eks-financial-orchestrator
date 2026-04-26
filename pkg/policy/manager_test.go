package policy

import (
	"context"
	"encoding/json"
	"testing"

	v1alpha1 "github.com/eks-financial-orchestrator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func newScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = v1alpha1.AddToScheme(s)
	return s
}

func newFakeClient(objs ...client.Object) client.Client {
	return fake.NewClientBuilder().
		WithScheme(newScheme()).
		WithObjects(objs...).
		WithStatusSubresource(&v1alpha1.FinancialPolicy{}).
		Build()
}

func newManager(objs ...client.Object) *DefaultPolicyManager {
	c := newFakeClient(objs...)
	return NewDefaultPolicyManager(c, log.Log)
}

func TestApplyPolicy_CreateNew(t *testing.T) {
	mgr := newManager()
	ctx := context.Background()

	p := validPolicy()
	if err := mgr.ApplyPolicy(ctx, p); err != nil {
		t.Fatalf("ApplyPolicy failed: %v", err)
	}

	// Verify version is set to 1.
	if p.Spec.Version != 1 {
		t.Errorf("expected version 1, got %d", p.Spec.Version)
	}

	// Verify the policy was created in the cluster.
	got := &v1alpha1.FinancialPolicy{}
	if err := mgr.client.Get(ctx, types.NamespacedName{Name: p.Name, Namespace: p.Namespace}, got); err != nil {
		t.Fatalf("failed to get created policy: %v", err)
	}
	if got.Spec.Version != 1 {
		t.Errorf("stored policy version: expected 1, got %d", got.Spec.Version)
	}

	// Verify version history annotation is initialized.
	raw, ok := got.Annotations[VersionHistoryAnnotation]
	if !ok {
		t.Fatal("expected version history annotation to be set")
	}
	var history []PolicyVersion
	if err := json.Unmarshal([]byte(raw), &history); err != nil {
		t.Fatalf("failed to unmarshal version history: %v", err)
	}
	if len(history) != 0 {
		t.Errorf("expected empty version history on create, got %d entries", len(history))
	}
}

func TestApplyPolicy_UpdateExisting(t *testing.T) {
	ctx := context.Background()

	existing := validPolicy()
	existing.Spec.Version = 1
	existing.Annotations = map[string]string{
		VersionHistoryAnnotation: "[]",
	}

	mgr := newManager(existing)

	// Create an updated policy with the same name/namespace.
	updated := validPolicy()
	updated.Spec.Budget.MonthlyLimit = 10000.0

	if err := mgr.ApplyPolicy(ctx, updated); err != nil {
		t.Fatalf("ApplyPolicy update failed: %v", err)
	}

	// Verify version was incremented.
	if updated.Spec.Version != 2 {
		t.Errorf("expected version 2, got %d", updated.Spec.Version)
	}

	// Verify the policy was updated in the cluster.
	got := &v1alpha1.FinancialPolicy{}
	if err := mgr.client.Get(ctx, types.NamespacedName{Name: updated.Name, Namespace: updated.Namespace}, got); err != nil {
		t.Fatalf("failed to get updated policy: %v", err)
	}
	if got.Spec.Budget.MonthlyLimit != 10000.0 {
		t.Errorf("expected monthly limit 10000, got %f", got.Spec.Budget.MonthlyLimit)
	}

	// Verify version history contains the previous spec.
	raw := got.Annotations[VersionHistoryAnnotation]
	var history []PolicyVersion
	if err := json.Unmarshal([]byte(raw), &history); err != nil {
		t.Fatalf("failed to unmarshal version history: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("expected 1 version history entry, got %d", len(history))
	}
	if history[0].Version != 1 {
		t.Errorf("expected history entry version 1, got %d", history[0].Version)
	}
	if history[0].Spec.Budget.MonthlyLimit != 5000.0 {
		t.Errorf("expected history entry monthly limit 5000, got %f", history[0].Spec.Budget.MonthlyLimit)
	}
}

func TestApplyPolicy_ValidationFailure(t *testing.T) {
	mgr := newManager()
	ctx := context.Background()

	p := validPolicy()
	p.Spec.TargetNamespace = "" // Invalid: required field.

	err := mgr.ApplyPolicy(ctx, p)
	if err == nil {
		t.Fatal("expected ApplyPolicy to fail for invalid policy")
	}
}

func TestApplyPolicy_VersionHistoryLimit(t *testing.T) {
	ctx := context.Background()

	// Create an existing policy with a full version history (10 entries).
	var history []PolicyVersion
	for i := 1; i <= maxVersionHistory; i++ {
		history = append(history, PolicyVersion{
			Version: i,
			Spec: v1alpha1.FinancialPolicySpec{
				TargetNamespace: "production",
				Budget: v1alpha1.BudgetSpec{
					MonthlyLimit: float64(i * 1000),
				},
			},
		})
	}
	historyJSON, _ := json.Marshal(history)

	existing := validPolicy()
	existing.Spec.Version = maxVersionHistory
	existing.Annotations = map[string]string{
		VersionHistoryAnnotation: string(historyJSON),
	}

	mgr := newManager(existing)

	updated := validPolicy()
	updated.Spec.Budget.MonthlyLimit = 99999.0

	if err := mgr.ApplyPolicy(ctx, updated); err != nil {
		t.Fatalf("ApplyPolicy failed: %v", err)
	}

	// Verify history is trimmed to maxVersionHistory.
	got := &v1alpha1.FinancialPolicy{}
	if err := mgr.client.Get(ctx, types.NamespacedName{Name: updated.Name, Namespace: updated.Namespace}, got); err != nil {
		t.Fatalf("failed to get policy: %v", err)
	}

	raw := got.Annotations[VersionHistoryAnnotation]
	var updatedHistory []PolicyVersion
	if err := json.Unmarshal([]byte(raw), &updatedHistory); err != nil {
		t.Fatalf("failed to unmarshal version history: %v", err)
	}
	if len(updatedHistory) != maxVersionHistory {
		t.Errorf("expected history length %d, got %d", maxVersionHistory, len(updatedHistory))
	}
	// The oldest entry should have been trimmed.
	if updatedHistory[0].Version != 2 {
		t.Errorf("expected oldest history entry version 2, got %d", updatedHistory[0].Version)
	}
}

func TestGetActivePolicy_ReturnsActivePhase(t *testing.T) {
	ctx := context.Background()

	p1 := &v1alpha1.FinancialPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "policy-1",
			Namespace: "default",
		},
		Spec: v1alpha1.FinancialPolicySpec{
			TargetNamespace: "production",
			Budget:          v1alpha1.BudgetSpec{MonthlyLimit: 1000},
		},
		Status: v1alpha1.FinancialPolicyStatus{
			Phase: v1alpha1.PolicyPhaseValidating,
		},
	}
	p2 := &v1alpha1.FinancialPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "policy-2",
			Namespace: "default",
		},
		Spec: v1alpha1.FinancialPolicySpec{
			TargetNamespace: "production",
			Budget:          v1alpha1.BudgetSpec{MonthlyLimit: 2000},
		},
		Status: v1alpha1.FinancialPolicyStatus{
			Phase: v1alpha1.PolicyPhaseActive,
		},
	}

	mgr := newManager(p1, p2)

	got, err := mgr.GetActivePolicy(ctx, "default")
	if err != nil {
		t.Fatalf("GetActivePolicy failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected a policy, got nil")
	}
	if got.Name != "policy-2" {
		t.Errorf("expected policy-2 (Active), got %s", got.Name)
	}
}

func TestGetActivePolicy_FallbackToFirst(t *testing.T) {
	ctx := context.Background()

	p1 := &v1alpha1.FinancialPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "policy-1",
			Namespace: "default",
		},
		Spec: v1alpha1.FinancialPolicySpec{
			TargetNamespace: "production",
			Budget:          v1alpha1.BudgetSpec{MonthlyLimit: 1000},
		},
		Status: v1alpha1.FinancialPolicyStatus{
			Phase: v1alpha1.PolicyPhaseValidating,
		},
	}

	mgr := newManager(p1)

	got, err := mgr.GetActivePolicy(ctx, "default")
	if err != nil {
		t.Fatalf("GetActivePolicy failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected a policy, got nil")
	}
	if got.Name != "policy-1" {
		t.Errorf("expected policy-1 as fallback, got %s", got.Name)
	}
}

func TestGetActivePolicy_EmptyNamespace(t *testing.T) {
	mgr := newManager()
	ctx := context.Background()

	got, err := mgr.GetActivePolicy(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("GetActivePolicy failed: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for empty namespace, got %+v", got)
	}
}

func TestListPolicies_AllNamespaces(t *testing.T) {
	ctx := context.Background()

	p1 := &v1alpha1.FinancialPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "policy-1",
			Namespace: "ns-a",
		},
		Spec: v1alpha1.FinancialPolicySpec{
			TargetNamespace: "production",
			Budget:          v1alpha1.BudgetSpec{MonthlyLimit: 1000},
		},
	}
	p2 := &v1alpha1.FinancialPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "policy-2",
			Namespace: "ns-b",
		},
		Spec: v1alpha1.FinancialPolicySpec{
			TargetNamespace: "staging",
			Budget:          v1alpha1.BudgetSpec{MonthlyLimit: 2000},
		},
	}

	mgr := newManager(p1, p2)

	policies, err := mgr.ListPolicies(ctx)
	if err != nil {
		t.Fatalf("ListPolicies failed: %v", err)
	}
	if len(policies) != 2 {
		t.Errorf("expected 2 policies, got %d", len(policies))
	}
}

func TestListPolicies_Empty(t *testing.T) {
	mgr := newManager()
	ctx := context.Background()

	policies, err := mgr.ListPolicies(ctx)
	if err != nil {
		t.Fatalf("ListPolicies failed: %v", err)
	}
	if len(policies) != 0 {
		t.Errorf("expected 0 policies, got %d", len(policies))
	}
}

func TestValidatePolicy_DelegatesToValidateFinancialPolicy(t *testing.T) {
	mgr := newManager()
	ctx := context.Background()

	p := validPolicy()
	errs, err := mgr.ValidatePolicy(ctx, p)
	if err != nil {
		t.Fatalf("ValidatePolicy returned error: %v", err)
	}
	if len(errs) != 0 {
		t.Errorf("expected no validation errors for valid policy, got %d", len(errs))
	}

	// Test with invalid policy.
	p.Spec.TargetNamespace = ""
	errs, err = mgr.ValidatePolicy(ctx, p)
	if err != nil {
		t.Fatalf("ValidatePolicy returned error: %v", err)
	}
	if len(errs) == 0 {
		t.Error("expected validation errors for invalid policy, got none")
	}
}

func TestGetPolicyHistory_ReturnsCorrectHistory(t *testing.T) {
	ctx := context.Background()

	// Create a policy with version 3 and history containing versions 1 and 2.
	history := []PolicyVersion{
		{
			Version: 1,
			Spec: v1alpha1.FinancialPolicySpec{
				TargetNamespace: "production",
				Budget: v1alpha1.BudgetSpec{
					MonthlyLimit: 1000.0,
					BreachAction: v1alpha1.BreachActionAlert,
				},
				Mode: v1alpha1.PolicyModeAdvisory,
			},
		},
		{
			Version: 2,
			Spec: v1alpha1.FinancialPolicySpec{
				TargetNamespace: "production",
				Budget: v1alpha1.BudgetSpec{
					MonthlyLimit: 3000.0,
					BreachAction: v1alpha1.BreachActionAlert,
				},
				Mode: v1alpha1.PolicyModeAdvisory,
			},
		},
	}
	historyJSON, _ := json.Marshal(history)

	existing := validPolicy()
	existing.Spec.Version = 3
	existing.Spec.Budget.MonthlyLimit = 5000.0
	existing.Annotations = map[string]string{
		VersionHistoryAnnotation: string(historyJSON),
	}

	mgr := newManager(existing)

	result, err := mgr.GetPolicyHistory(ctx, "test-policy")
	if err != nil {
		t.Fatalf("GetPolicyHistory failed: %v", err)
	}

	// Should have 3 entries: versions 1, 2, and 3 (current).
	if len(result) != 3 {
		t.Fatalf("expected 3 history entries, got %d", len(result))
	}

	// Verify sorted by version ascending.
	for i := 0; i < len(result)-1; i++ {
		if result[i].Version >= result[i+1].Version {
			t.Errorf("history not sorted: version %d at index %d, version %d at index %d",
				result[i].Version, i, result[i+1].Version, i+1)
		}
	}

	// Verify version 1 spec.
	if result[0].Version != 1 {
		t.Errorf("expected version 1, got %d", result[0].Version)
	}
	if result[0].Spec.Budget.MonthlyLimit != 1000.0 {
		t.Errorf("expected version 1 monthly limit 1000, got %f", result[0].Spec.Budget.MonthlyLimit)
	}

	// Verify version 2 spec.
	if result[1].Version != 2 {
		t.Errorf("expected version 2, got %d", result[1].Version)
	}
	if result[1].Spec.Budget.MonthlyLimit != 3000.0 {
		t.Errorf("expected version 2 monthly limit 3000, got %f", result[1].Spec.Budget.MonthlyLimit)
	}

	// Verify current version (3) is included.
	if result[2].Version != 3 {
		t.Errorf("expected version 3, got %d", result[2].Version)
	}
	if result[2].Spec.Budget.MonthlyLimit != 5000.0 {
		t.Errorf("expected version 3 monthly limit 5000, got %f", result[2].Spec.Budget.MonthlyLimit)
	}
}

func TestGetPolicyHistory_EmptyHistory(t *testing.T) {
	ctx := context.Background()

	existing := validPolicy()
	existing.Spec.Version = 1
	existing.Annotations = map[string]string{
		VersionHistoryAnnotation: "[]",
	}

	mgr := newManager(existing)

	result, err := mgr.GetPolicyHistory(ctx, "test-policy")
	if err != nil {
		t.Fatalf("GetPolicyHistory failed: %v", err)
	}

	// Should have 1 entry: just the current version.
	if len(result) != 1 {
		t.Fatalf("expected 1 history entry, got %d", len(result))
	}
	if result[0].Version != 1 {
		t.Errorf("expected version 1, got %d", result[0].Version)
	}
}

func TestGetPolicyHistory_PolicyNotFound(t *testing.T) {
	mgr := newManager()
	ctx := context.Background()

	_, err := mgr.GetPolicyHistory(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent policy")
	}
}

func TestRollbackPolicy_RestoresSpecFromVersion(t *testing.T) {
	ctx := context.Background()

	// Create a policy at version 3 with history containing versions 1 and 2.
	history := []PolicyVersion{
		{
			Version: 1,
			Spec: v1alpha1.FinancialPolicySpec{
				TargetNamespace: "production",
				Budget: v1alpha1.BudgetSpec{
					MonthlyLimit: 1000.0,
					BreachAction: v1alpha1.BreachActionAlert,
				},
				Mode: v1alpha1.PolicyModeAdvisory,
			},
		},
		{
			Version: 2,
			Spec: v1alpha1.FinancialPolicySpec{
				TargetNamespace: "production",
				Budget: v1alpha1.BudgetSpec{
					MonthlyLimit: 3000.0,
					BreachAction: v1alpha1.BreachActionThrottle,
				},
				Mode: v1alpha1.PolicyModeAutomatic,
			},
		},
	}
	historyJSON, _ := json.Marshal(history)

	existing := validPolicy()
	existing.Spec.Version = 3
	existing.Spec.Budget.MonthlyLimit = 5000.0
	existing.Annotations = map[string]string{
		VersionHistoryAnnotation: string(historyJSON),
	}

	mgr := newManager(existing)

	// Rollback to version 1.
	if err := mgr.RollbackPolicy(ctx, "test-policy", 1); err != nil {
		t.Fatalf("RollbackPolicy failed: %v", err)
	}

	// Verify the policy was updated.
	got := &v1alpha1.FinancialPolicy{}
	if err := mgr.client.Get(ctx, types.NamespacedName{Name: "test-policy", Namespace: "default"}, got); err != nil {
		t.Fatalf("failed to get policy after rollback: %v", err)
	}

	// Spec should match version 1's spec.
	if got.Spec.Budget.MonthlyLimit != 1000.0 {
		t.Errorf("expected monthly limit 1000 (from version 1), got %f", got.Spec.Budget.MonthlyLimit)
	}
	if got.Spec.Budget.BreachAction != v1alpha1.BreachActionAlert {
		t.Errorf("expected breach action 'alert' (from version 1), got %q", got.Spec.Budget.BreachAction)
	}
	if got.Spec.Mode != v1alpha1.PolicyModeAdvisory {
		t.Errorf("expected mode 'advisory' (from version 1), got %q", got.Spec.Mode)
	}

	// Version should be incremented (rollback creates a new version).
	if got.Spec.Version != 4 {
		t.Errorf("expected version 4 after rollback, got %d", got.Spec.Version)
	}

	// Status phase should be RolledBack.
	if got.Status.Phase != v1alpha1.PolicyPhaseRolledBack {
		t.Errorf("expected status phase RolledBack, got %q", got.Status.Phase)
	}

	// Version history should contain the previous version 3 spec.
	raw := got.Annotations[VersionHistoryAnnotation]
	var updatedHistory []PolicyVersion
	if err := json.Unmarshal([]byte(raw), &updatedHistory); err != nil {
		t.Fatalf("failed to unmarshal version history: %v", err)
	}
	// Should have 3 entries: versions 1, 2, and 3 (the pre-rollback spec).
	if len(updatedHistory) != 3 {
		t.Fatalf("expected 3 history entries after rollback, got %d", len(updatedHistory))
	}
	// The last entry should be the version 3 spec that was current before rollback.
	lastEntry := updatedHistory[len(updatedHistory)-1]
	if lastEntry.Version != 3 {
		t.Errorf("expected last history entry version 3, got %d", lastEntry.Version)
	}
	if lastEntry.Spec.Budget.MonthlyLimit != 5000.0 {
		t.Errorf("expected last history entry monthly limit 5000, got %f", lastEntry.Spec.Budget.MonthlyLimit)
	}
}

func TestRollbackPolicy_NonExistentVersion(t *testing.T) {
	ctx := context.Background()

	history := []PolicyVersion{
		{
			Version: 1,
			Spec: v1alpha1.FinancialPolicySpec{
				TargetNamespace: "production",
				Budget: v1alpha1.BudgetSpec{
					MonthlyLimit: 1000.0,
					BreachAction: v1alpha1.BreachActionAlert,
				},
				Mode: v1alpha1.PolicyModeAdvisory,
			},
		},
	}
	historyJSON, _ := json.Marshal(history)

	existing := validPolicy()
	existing.Spec.Version = 2
	existing.Annotations = map[string]string{
		VersionHistoryAnnotation: string(historyJSON),
	}

	mgr := newManager(existing)

	err := mgr.RollbackPolicy(ctx, "test-policy", 99)
	if err == nil {
		t.Fatal("expected error for non-existent version")
	}
}

func TestRollbackPolicy_NonExistentPolicy(t *testing.T) {
	mgr := newManager()
	ctx := context.Background()

	err := mgr.RollbackPolicy(ctx, "nonexistent", 1)
	if err == nil {
		t.Fatal("expected error for non-existent policy")
	}
}
