package policy

import (
	"fmt"
	"sort"

	v1alpha1 "github.com/eks-financial-orchestrator/api/v1alpha1"
)

// validBreachActions is the set of allowed breach_action values.
var validBreachActions = map[v1alpha1.BreachAction]bool{
	v1alpha1.BreachActionAlert:            true,
	v1alpha1.BreachActionThrottle:         true,
	v1alpha1.BreachActionBlockDeployments: true,
}

// validModes is the set of allowed mode values.
var validModes = map[v1alpha1.PolicyMode]bool{
	v1alpha1.PolicyModeAutomatic: true,
	v1alpha1.PolicyModeAdvisory:  true,
}

// validChannelTypes is the set of allowed alerting channel types.
var validChannelTypes = map[string]bool{
	"slack":     true,
	"email":     true,
	"pagerduty": true,
	"sns":       true,
}

// ValidateFinancialPolicy validates a FinancialPolicy and returns a slice of
// ValidationError. An empty slice indicates the policy is valid.
func ValidateFinancialPolicy(policy *v1alpha1.FinancialPolicy) []ValidationError {
	var errs []ValidationError

	// Validate required fields.
	if policy.Spec.TargetNamespace == "" {
		errs = append(errs, ValidationError{
			Field:   "spec.targetNamespace",
			Message: "targetNamespace must be non-empty",
		})
	}

	if policy.Spec.Budget.MonthlyLimit <= 0 {
		errs = append(errs, ValidationError{
			Field:   "spec.budget.monthly_limit",
			Message: "monthly_limit must be greater than 0",
		})
	}

	// Validate enum: breach_action.
	if policy.Spec.Budget.BreachAction != "" && !validBreachActions[policy.Spec.Budget.BreachAction] {
		errs = append(errs, ValidationError{
			Field:   "spec.budget.breach_action",
			Message: fmt.Sprintf("breach_action must be one of: alert, throttle, block_deployments; got %q", policy.Spec.Budget.BreachAction),
		})
	}

	// Validate enum: mode.
	if policy.Spec.Mode != "" && !validModes[policy.Spec.Mode] {
		errs = append(errs, ValidationError{
			Field:   "spec.mode",
			Message: fmt.Sprintf("mode must be one of: automatic, advisory; got %q", policy.Spec.Mode),
		})
	}

	// Validate instance mix numeric ranges.
	if policy.Spec.InstanceMix != nil {
		if policy.Spec.InstanceMix.MinOnDemandPercent < 0 || policy.Spec.InstanceMix.MinOnDemandPercent > 100 {
			errs = append(errs, ValidationError{
				Field:   "spec.instanceMix.min_on_demand_percent",
				Message: fmt.Sprintf("min_on_demand_percent must be between 0 and 100; got %v", policy.Spec.InstanceMix.MinOnDemandPercent),
			})
		}
		if policy.Spec.InstanceMix.MaxSpotPercent < 0 || policy.Spec.InstanceMix.MaxSpotPercent > 100 {
			errs = append(errs, ValidationError{
				Field:   "spec.instanceMix.max_spot_percent",
				Message: fmt.Sprintf("max_spot_percent must be between 0 and 100; got %v", policy.Spec.InstanceMix.MaxSpotPercent),
			})
		}
	}

	// Validate alert thresholds.
	thresholds := policy.Spec.Budget.AlertThresholds
	for i, t := range thresholds {
		if t < 0 || t > 100 {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("spec.budget.alert_thresholds[%d]", i),
				Message: fmt.Sprintf("alert threshold must be between 0 and 100; got %v", t),
			})
		}
	}
	if len(thresholds) > 1 && !sort.Float64sAreSorted(thresholds) {
		errs = append(errs, ValidationError{
			Field:   "spec.budget.alert_thresholds",
			Message: "alert_thresholds must be sorted in ascending order",
		})
	}

	// Validate alerting channels.
	if policy.Spec.Alerting != nil {
		for i, ch := range policy.Spec.Alerting.Channels {
			if !validChannelTypes[ch.Type] {
				errs = append(errs, ValidationError{
					Field:   fmt.Sprintf("spec.alerting.channels[%d].type", i),
					Message: fmt.Sprintf("channel type must be one of: slack, email, pagerduty, sns; got %q", ch.Type),
				})
			}
		}

		// Validate silence windows.
		for i, sw := range policy.Spec.Alerting.SilenceWindows {
			if !sw.Start.Time.Before(sw.End.Time) {
				errs = append(errs, ValidationError{
					Field:   fmt.Sprintf("spec.alerting.silence_windows[%d]", i),
					Message: "silence window start must be before end",
				})
			}
		}
	}

	return errs
}
