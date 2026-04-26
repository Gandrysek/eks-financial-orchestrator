package policy

import (
	"testing"
	"time"

	v1alpha1 "github.com/eks-financial-orchestrator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// validPolicy returns a minimal valid FinancialPolicy for use in tests.
func validPolicy() *v1alpha1.FinancialPolicy {
	return &v1alpha1.FinancialPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-policy",
			Namespace: "default",
		},
		Spec: v1alpha1.FinancialPolicySpec{
			TargetNamespace: "production",
			Budget: v1alpha1.BudgetSpec{
				MonthlyLimit:    5000.0,
				AlertThresholds: []float64{50, 80, 90, 100},
				BreachAction:    v1alpha1.BreachActionAlert,
			},
			Mode: v1alpha1.PolicyModeAdvisory,
		},
	}
}

func TestValidateFinancialPolicy_ValidPolicy(t *testing.T) {
	p := validPolicy()
	errs := ValidateFinancialPolicy(p)
	if len(errs) != 0 {
		t.Errorf("expected no validation errors for valid policy, got %d: %+v", len(errs), errs)
	}
}

func TestValidateFinancialPolicy_MissingTargetNamespace(t *testing.T) {
	p := validPolicy()
	p.Spec.TargetNamespace = ""
	errs := ValidateFinancialPolicy(p)
	assertHasFieldError(t, errs, "spec.targetNamespace")
}

func TestValidateFinancialPolicy_NegativeMonthlyLimit(t *testing.T) {
	p := validPolicy()
	p.Spec.Budget.MonthlyLimit = -100.0
	errs := ValidateFinancialPolicy(p)
	assertHasFieldError(t, errs, "spec.budget.monthly_limit")
}

func TestValidateFinancialPolicy_ZeroMonthlyLimit(t *testing.T) {
	p := validPolicy()
	p.Spec.Budget.MonthlyLimit = 0
	errs := ValidateFinancialPolicy(p)
	assertHasFieldError(t, errs, "spec.budget.monthly_limit")
}

func TestValidateFinancialPolicy_InvalidBreachAction(t *testing.T) {
	p := validPolicy()
	p.Spec.Budget.BreachAction = "invalid_action"
	errs := ValidateFinancialPolicy(p)
	assertHasFieldError(t, errs, "spec.budget.breach_action")
}

func TestValidateFinancialPolicy_InvalidMode(t *testing.T) {
	p := validPolicy()
	p.Spec.Mode = "manual"
	errs := ValidateFinancialPolicy(p)
	assertHasFieldError(t, errs, "spec.mode")
}

func TestValidateFinancialPolicy_InstanceMixOutOfRange(t *testing.T) {
	p := validPolicy()
	p.Spec.InstanceMix = &v1alpha1.InstanceMixSpec{
		MinOnDemandPercent: 150,
		MaxSpotPercent:     200,
	}
	errs := ValidateFinancialPolicy(p)
	assertHasFieldError(t, errs, "spec.instanceMix.min_on_demand_percent")
	assertHasFieldError(t, errs, "spec.instanceMix.max_spot_percent")
}

func TestValidateFinancialPolicy_InstanceMixNegative(t *testing.T) {
	p := validPolicy()
	p.Spec.InstanceMix = &v1alpha1.InstanceMixSpec{
		MinOnDemandPercent: -5,
		MaxSpotPercent:     -10,
	}
	errs := ValidateFinancialPolicy(p)
	assertHasFieldError(t, errs, "spec.instanceMix.min_on_demand_percent")
	assertHasFieldError(t, errs, "spec.instanceMix.max_spot_percent")
}

func TestValidateFinancialPolicy_UnsortedAlertThresholds(t *testing.T) {
	p := validPolicy()
	p.Spec.Budget.AlertThresholds = []float64{90, 50, 80, 100}
	errs := ValidateFinancialPolicy(p)
	assertHasFieldError(t, errs, "spec.budget.alert_thresholds")
}

func TestValidateFinancialPolicy_AlertThresholdOutOfRange(t *testing.T) {
	p := validPolicy()
	p.Spec.Budget.AlertThresholds = []float64{50, 80, 110}
	errs := ValidateFinancialPolicy(p)
	assertHasFieldError(t, errs, "spec.budget.alert_thresholds[2]")
}

func TestValidateFinancialPolicy_InvalidChannelType(t *testing.T) {
	p := validPolicy()
	p.Spec.Alerting = &v1alpha1.AlertingSpec{
		Channels: []v1alpha1.AlertingChannelSpec{
			{Type: "slack"},
			{Type: "telegram"},
		},
	}
	errs := ValidateFinancialPolicy(p)
	assertHasFieldError(t, errs, "spec.alerting.channels[1].type")
}

func TestValidateFinancialPolicy_SilenceWindowStartAfterEnd(t *testing.T) {
	now := time.Now()
	p := validPolicy()
	p.Spec.Alerting = &v1alpha1.AlertingSpec{
		SilenceWindows: []v1alpha1.SilenceWindowSpec{
			{
				Start:  metav1.NewTime(now.Add(2 * time.Hour)),
				End:    metav1.NewTime(now),
				Reason: "maintenance",
			},
		},
	}
	errs := ValidateFinancialPolicy(p)
	assertHasFieldError(t, errs, "spec.alerting.silence_windows[0]")
}

func TestValidateFinancialPolicy_MultipleErrors(t *testing.T) {
	p := &v1alpha1.FinancialPolicy{
		Spec: v1alpha1.FinancialPolicySpec{
			TargetNamespace: "",
			Budget: v1alpha1.BudgetSpec{
				MonthlyLimit: -100,
				BreachAction: "invalid",
			},
			Mode: "manual",
			InstanceMix: &v1alpha1.InstanceMixSpec{
				MinOnDemandPercent: 200,
			},
		},
	}
	errs := ValidateFinancialPolicy(p)
	if len(errs) < 4 {
		t.Errorf("expected at least 4 validation errors, got %d: %+v", len(errs), errs)
	}
	assertHasFieldError(t, errs, "spec.targetNamespace")
	assertHasFieldError(t, errs, "spec.budget.monthly_limit")
	assertHasFieldError(t, errs, "spec.budget.breach_action")
	assertHasFieldError(t, errs, "spec.mode")
	assertHasFieldError(t, errs, "spec.instanceMix.min_on_demand_percent")
}

func TestValidateFinancialPolicy_EmptyBreachActionIsValid(t *testing.T) {
	p := validPolicy()
	p.Spec.Budget.BreachAction = ""
	errs := ValidateFinancialPolicy(p)
	assertNoFieldError(t, errs, "spec.budget.breach_action")
}

func TestValidateFinancialPolicy_EmptyModeIsValid(t *testing.T) {
	p := validPolicy()
	p.Spec.Mode = ""
	errs := ValidateFinancialPolicy(p)
	assertNoFieldError(t, errs, "spec.mode")
}

func TestValidateFinancialPolicy_ValidChannelTypes(t *testing.T) {
	p := validPolicy()
	p.Spec.Alerting = &v1alpha1.AlertingSpec{
		Channels: []v1alpha1.AlertingChannelSpec{
			{Type: "slack"},
			{Type: "email"},
			{Type: "pagerduty"},
			{Type: "sns"},
		},
	}
	errs := ValidateFinancialPolicy(p)
	if len(errs) != 0 {
		t.Errorf("expected no errors for valid channel types, got %d: %+v", len(errs), errs)
	}
}

func TestValidateFinancialPolicy_ValidSilenceWindow(t *testing.T) {
	now := time.Now()
	p := validPolicy()
	p.Spec.Alerting = &v1alpha1.AlertingSpec{
		SilenceWindows: []v1alpha1.SilenceWindowSpec{
			{
				Start:  metav1.NewTime(now),
				End:    metav1.NewTime(now.Add(2 * time.Hour)),
				Reason: "maintenance",
			},
		},
	}
	errs := ValidateFinancialPolicy(p)
	if len(errs) != 0 {
		t.Errorf("expected no errors for valid silence window, got %d: %+v", len(errs), errs)
	}
}

// assertHasFieldError checks that at least one ValidationError has the given field.
func assertHasFieldError(t *testing.T, errs []ValidationError, field string) {
	t.Helper()
	for _, e := range errs {
		if e.Field == field {
			return
		}
	}
	t.Errorf("expected validation error for field %q, but none found in %+v", field, errs)
}

// assertNoFieldError checks that no ValidationError has the given field.
func assertNoFieldError(t *testing.T, errs []ValidationError, field string) {
	t.Helper()
	for _, e := range errs {
		if e.Field == field {
			t.Errorf("expected no validation error for field %q, but found: %+v", field, e)
			return
		}
	}
}
