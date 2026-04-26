package generators

import (
	"sort"

	v1alpha1 "github.com/eks-financial-orchestrator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"pgregory.net/rapid"
)

// PolicyMode generates a random PolicyMode (automatic or advisory).
func PolicyMode(t *rapid.T) v1alpha1.PolicyMode {
	modes := []v1alpha1.PolicyMode{
		v1alpha1.PolicyModeAutomatic,
		v1alpha1.PolicyModeAdvisory,
	}
	return modes[rapid.IntRange(0, len(modes)-1).Draw(t, "mode_index")]
}

// BreachAction generates a random BreachAction.
func BreachAction(t *rapid.T) v1alpha1.BreachAction {
	actions := []v1alpha1.BreachAction{
		v1alpha1.BreachActionAlert,
		v1alpha1.BreachActionThrottle,
		v1alpha1.BreachActionBlockDeployments,
	}
	return actions[rapid.IntRange(0, len(actions)-1).Draw(t, "action_index")]
}

// ValidFinancialPolicy generates a valid FinancialPolicy with realistic values.
func ValidFinancialPolicy(t *rapid.T) *v1alpha1.FinancialPolicy {
	ns := rapid.StringMatching(`[a-z]{3,20}`).Draw(t, "targetNamespace")
	monthlyLimit := rapid.Float64Range(1.0, 100000.0).Draw(t, "monthly_limit")

	// Generate sorted alert thresholds as a subset of [50, 80, 90, 100].
	allThresholds := []float64{50, 80, 90, 100}
	thresholds := sortedSubset(t, allThresholds)

	breachAction := BreachAction(t)
	mode := PolicyMode(t)
	version := rapid.IntRange(1, 100).Draw(t, "version")

	spec := v1alpha1.FinancialPolicySpec{
		TargetNamespace: ns,
		Budget: v1alpha1.BudgetSpec{
			MonthlyLimit:    monthlyLimit,
			AlertThresholds: thresholds,
			BreachAction:    breachAction,
		},
		Mode:    mode,
		Version: version,
	}

	// Optionally add InstanceMixSpec.
	if rapid.Bool().Draw(t, "has_instance_mix") {
		minOD := rapid.Float64Range(0, 100).Draw(t, "min_on_demand_percent")
		maxSpot := rapid.Float64Range(0, 100).Draw(t, "max_spot_percent")
		instanceTypes := rapid.SliceOfN(
			rapid.StringMatching(`[a-z][0-9][a-z]?\.[a-z]+`),
			0, 5,
		).Draw(t, "allowed_instance_types")
		spec.InstanceMix = &v1alpha1.InstanceMixSpec{
			MinOnDemandPercent:   minOD,
			MaxSpotPercent:       maxSpot,
			AllowedInstanceTypes: instanceTypes,
		}
	}

	// Optionally add AlertingSpec.
	if rapid.Bool().Draw(t, "has_alerting") {
		spec.Alerting = alertingSpec(t)
	}

	return &v1alpha1.FinancialPolicy{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "finops.eks.io/v1alpha1",
			Kind:       "FinancialPolicy",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      rapid.StringMatching(`[a-z]{3,15}`).Draw(t, "policy_name"),
			Namespace: ns,
		},
		Spec: spec,
	}
}

// InvalidFinancialPolicy generates an invalid FinancialPolicy that should fail validation.
func InvalidFinancialPolicy(t *rapid.T) *v1alpha1.FinancialPolicy {
	defect := rapid.IntRange(0, 3).Draw(t, "defect_type")

	policy := &v1alpha1.FinancialPolicy{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "finops.eks.io/v1alpha1",
			Kind:       "FinancialPolicy",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      rapid.StringMatching(`[a-z]{3,15}`).Draw(t, "policy_name"),
			Namespace: "default",
		},
	}

	switch defect {
	case 0:
		// Missing targetNamespace.
		policy.Spec = v1alpha1.FinancialPolicySpec{
			TargetNamespace: "",
			Budget: v1alpha1.BudgetSpec{
				MonthlyLimit: rapid.Float64Range(1.0, 100000.0).Draw(t, "monthly_limit"),
			},
		}
	case 1:
		// Negative monthly limit.
		policy.Spec = v1alpha1.FinancialPolicySpec{
			TargetNamespace: rapid.StringMatching(`[a-z]{3,20}`).Draw(t, "targetNamespace"),
			Budget: v1alpha1.BudgetSpec{
				MonthlyLimit: -rapid.Float64Range(1.0, 100000.0).Draw(t, "monthly_limit"),
			},
		}
	case 2:
		// Out-of-range instance mix percentages.
		policy.Spec = v1alpha1.FinancialPolicySpec{
			TargetNamespace: rapid.StringMatching(`[a-z]{3,20}`).Draw(t, "targetNamespace"),
			Budget: v1alpha1.BudgetSpec{
				MonthlyLimit: rapid.Float64Range(1.0, 100000.0).Draw(t, "monthly_limit"),
			},
			InstanceMix: &v1alpha1.InstanceMixSpec{
				MinOnDemandPercent: rapid.Float64Range(101, 200).Draw(t, "min_on_demand_percent"),
				MaxSpotPercent:     rapid.Float64Range(101, 200).Draw(t, "max_spot_percent"),
			},
		}
	case 3:
		// Zero monthly limit (edge case — may be considered invalid).
		policy.Spec = v1alpha1.FinancialPolicySpec{
			TargetNamespace: rapid.StringMatching(`[a-z]{3,20}`).Draw(t, "targetNamespace"),
			Budget: v1alpha1.BudgetSpec{
				MonthlyLimit: 0,
			},
		}
	}

	return policy
}

// sortedSubset picks a random subset of the given values and returns them sorted.
func sortedSubset(t *rapid.T, values []float64) []float64 {
	n := len(values)
	// Use a bitmask to select a subset (1 to 2^n - 1 to ensure at least one element).
	mask := rapid.IntRange(1, (1<<n)-1).Draw(t, "threshold_mask")
	var result []float64
	for i := 0; i < n; i++ {
		if mask&(1<<i) != 0 {
			result = append(result, values[i])
		}
	}
	sort.Float64s(result)
	return result
}

// alertingSpec generates a random AlertingSpec.
func alertingSpec(t *rapid.T) *v1alpha1.AlertingSpec {
	channelTypes := []string{"slack", "email", "pagerduty", "sns"}

	numChannels := rapid.IntRange(1, 3).Draw(t, "num_channels")
	channels := make([]v1alpha1.AlertingChannelSpec, numChannels)
	for i := 0; i < numChannels; i++ {
		chType := channelTypes[rapid.IntRange(0, len(channelTypes)-1).Draw(t, "channel_type")]
		channels[i] = v1alpha1.AlertingChannelSpec{
			Type:   chType,
			Config: map[string]string{"endpoint": rapid.StringMatching(`https://[a-z]{5,10}\\.example\\.com`).Draw(t, "endpoint")},
		}
	}

	spec := &v1alpha1.AlertingSpec{
		Channels: channels,
	}

	// Optionally add silence windows.
	if rapid.Bool().Draw(t, "has_silence_windows") {
		numWindows := rapid.IntRange(1, 2).Draw(t, "num_silence_windows")
		windows := make([]v1alpha1.SilenceWindowSpec, numWindows)
		for i := 0; i < numWindows; i++ {
			start := metav1.Now()
			end := metav1.NewTime(start.Add(3600_000_000_000)) // 1 hour later
			windows[i] = v1alpha1.SilenceWindowSpec{
				Start:  start,
				End:    end,
				Reason: rapid.StringMatching(`[a-z ]{5,30}`).Draw(t, "silence_reason"),
			}
		}
		spec.SilenceWindows = windows
	}

	return spec
}
