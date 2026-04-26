package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PolicyMode defines the execution mode for automated actions.
// +kubebuilder:validation:Enum=automatic;advisory
type PolicyMode string

const (
	// PolicyModeAutomatic applies optimization recommendations without manual confirmation.
	PolicyModeAutomatic PolicyMode = "automatic"
	// PolicyModeAdvisory generates recommendations and waits for manual confirmation.
	PolicyModeAdvisory PolicyMode = "advisory"
)

// BreachAction defines the action to take when a budget is exceeded.
// +kubebuilder:validation:Enum=alert;throttle;block_deployments
type BreachAction string

const (
	BreachActionAlert            BreachAction = "alert"
	BreachActionThrottle         BreachAction = "throttle"
	BreachActionBlockDeployments BreachAction = "block_deployments"
)

// PolicyPhase describes the current lifecycle phase of a FinancialPolicy.
type PolicyPhase string

const (
	PolicyPhaseActive     PolicyPhase = "Active"
	PolicyPhaseValidating PolicyPhase = "Validating"
	PolicyPhaseError      PolicyPhase = "Error"
	PolicyPhaseRolledBack PolicyPhase = "RolledBack"
)

// BudgetSpec defines budget constraints for a namespace.
type BudgetSpec struct {
	// MonthlyLimit is the monthly budget limit in dollars.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Minimum=0
	MonthlyLimit float64 `json:"monthly_limit"`

	// AlertThresholds are percentage thresholds for alerts (e.g., [50, 80, 90, 100]).
	// +optional
	AlertThresholds []float64 `json:"alert_thresholds,omitempty"`

	// BreachAction is the action to take when the budget is exceeded.
	// +optional
	// +kubebuilder:validation:Enum=alert;throttle;block_deployments
	BreachAction BreachAction `json:"breach_action,omitempty"`
}

// InstanceMixSpec defines constraints on the instance mix.
type InstanceMixSpec struct {
	// MinOnDemandPercent is the minimum percentage of On-Demand instances.
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=100
	MinOnDemandPercent float64 `json:"min_on_demand_percent,omitempty"`

	// MaxSpotPercent is the maximum percentage of Spot instances.
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=100
	MaxSpotPercent float64 `json:"max_spot_percent,omitempty"`

	// AllowedInstanceTypes is the list of allowed EC2 instance types.
	// +optional
	AllowedInstanceTypes []string `json:"allowed_instance_types,omitempty"`
}

// AlertingChannelSpec defines a notification channel configuration.
type AlertingChannelSpec struct {
	// Type is the notification channel type.
	// +kubebuilder:validation:Enum=slack;email;pagerduty;sns
	Type string `json:"type"`

	// Config holds channel-specific configuration.
	// +kubebuilder:pruning:PreserveUnknownFields
	// +optional
	Config map[string]string `json:"config,omitempty"`
}

// SilenceWindowSpec defines a time window during which alerts are suppressed.
type SilenceWindowSpec struct {
	// Start is the beginning of the silence window.
	Start metav1.Time `json:"start"`

	// End is the end of the silence window.
	End metav1.Time `json:"end"`

	// Reason describes why alerts are silenced during this window.
	// +optional
	Reason string `json:"reason,omitempty"`
}

// AlertingSpec defines alerting configuration for a policy.
type AlertingSpec struct {
	// Channels is the list of notification channels.
	// +optional
	Channels []AlertingChannelSpec `json:"channels,omitempty"`

	// SilenceWindows is the list of silence windows.
	// +optional
	SilenceWindows []SilenceWindowSpec `json:"silence_windows,omitempty"`
}

// FinancialPolicySpec defines the desired state of a FinancialPolicy.
type FinancialPolicySpec struct {
	// TargetNamespace is the Kubernetes namespace this policy applies to.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	TargetNamespace string `json:"targetNamespace"`

	// Budget defines budget constraints for the namespace.
	// +kubebuilder:validation:Required
	Budget BudgetSpec `json:"budget"`

	// InstanceMix defines constraints on the instance mix.
	// +optional
	InstanceMix *InstanceMixSpec `json:"instanceMix,omitempty"`

	// Mode is the execution mode for automated actions.
	// +optional
	// +kubebuilder:default=advisory
	// +kubebuilder:validation:Enum=automatic;advisory
	Mode PolicyMode `json:"mode,omitempty"`

	// Alerting defines alerting configuration.
	// +optional
	Alerting *AlertingSpec `json:"alerting,omitempty"`

	// Version is the policy version number, auto-incremented on updates.
	// +optional
	Version int `json:"version,omitempty"`
}

// PolicyCondition describes the state of a FinancialPolicy at a certain point.
type PolicyCondition struct {
	// Type of the condition.
	Type string `json:"type"`

	// Status of the condition (True, False, Unknown).
	Status string `json:"status"`

	// LastTransitionTime is the last time the condition transitioned.
	// +optional
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty"`

	// Reason is a brief machine-readable explanation for the condition.
	// +optional
	Reason string `json:"reason,omitempty"`

	// Message is a human-readable description of the condition.
	// +optional
	Message string `json:"message,omitempty"`
}

// FinancialPolicyStatus defines the observed state of a FinancialPolicy.
type FinancialPolicyStatus struct {
	// Phase is the current lifecycle phase of the policy.
	// +optional
	// +kubebuilder:validation:Enum=Active;Validating;Error;RolledBack
	Phase PolicyPhase `json:"phase,omitempty"`

	// CurrentCost is the current cost in dollars for the target namespace.
	// +optional
	CurrentCost float64 `json:"currentCost,omitempty"`

	// BudgetUsagePercent is the current budget usage as a percentage.
	// +optional
	BudgetUsagePercent float64 `json:"budgetUsagePercent,omitempty"`

	// LastEvaluated is the last time the policy was evaluated.
	// +optional
	LastEvaluated *metav1.Time `json:"lastEvaluated,omitempty"`

	// Conditions represent the latest available observations of the policy's state.
	// +optional
	Conditions []PolicyCondition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=fp
// +kubebuilder:printcolumn:name="Namespace",type=string,JSONPath=`.spec.targetNamespace`
// +kubebuilder:printcolumn:name="Budget",type=number,JSONPath=`.spec.budget.monthly_limit`
// +kubebuilder:printcolumn:name="Mode",type=string,JSONPath=`.spec.mode`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Usage%",type=number,JSONPath=`.status.budgetUsagePercent`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// FinancialPolicy is the Schema for the financialpolicies API.
type FinancialPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   FinancialPolicySpec   `json:"spec,omitempty"`
	Status FinancialPolicyStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// FinancialPolicyList contains a list of FinancialPolicy.
type FinancialPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []FinancialPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&FinancialPolicy{}, &FinancialPolicyList{})
}
