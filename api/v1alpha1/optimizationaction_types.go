package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ActionType defines the type of optimization action.
// +kubebuilder:validation:Enum=instance_mix_change;spot_migration;savings_plan_recommendation;budget_enforcement
type ActionType string

const (
	ActionTypeInstanceMixChange         ActionType = "instance_mix_change"
	ActionTypeSpotMigration             ActionType = "spot_migration"
	ActionTypeSavingsPlanRecommendation ActionType = "savings_plan_recommendation"
	ActionTypeBudgetEnforcement         ActionType = "budget_enforcement"
)

// ActionResult defines the result of an optimization action.
// +kubebuilder:validation:Enum=pending;approved;applied;failed;rolled_back
type ActionResult string

const (
	ActionResultPending    ActionResult = "pending"
	ActionResultApproved   ActionResult = "approved"
	ActionResultApplied    ActionResult = "applied"
	ActionResultFailed     ActionResult = "failed"
	ActionResultRolledBack ActionResult = "rolled_back"
)

// OptimizationActionSpec defines the desired state of an OptimizationAction.
type OptimizationActionSpec struct {
	// ActionType is the type of optimization action.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=instance_mix_change;spot_migration;savings_plan_recommendation;budget_enforcement
	ActionType ActionType `json:"actionType"`

	// PolicyRef is the name of the FinancialPolicy that triggered this action.
	// +kubebuilder:validation:Required
	PolicyRef string `json:"policyRef"`

	// TargetNamespace is the namespace affected by this action.
	// +optional
	TargetNamespace string `json:"targetNamespace,omitempty"`

	// Reason describes why this action was generated.
	// +kubebuilder:validation:Required
	Reason string `json:"reason"`

	// ExpectedSavings is the estimated savings in dollars from this action.
	// +optional
	// +kubebuilder:validation:Minimum=0
	ExpectedSavings float64 `json:"expectedSavings,omitempty"`

	// AffectedNodes is the list of node names affected by this action.
	// +optional
	AffectedNodes []string `json:"affectedNodes,omitempty"`

	// CurrentMix describes the current instance mix percentages.
	// +optional
	CurrentMix map[string]float64 `json:"currentMix,omitempty"`

	// RecommendedMix describes the recommended instance mix percentages.
	// +optional
	RecommendedMix map[string]float64 `json:"recommendedMix,omitempty"`

	// RequiresApproval indicates whether this action needs manual approval.
	// +optional
	RequiresApproval bool `json:"requiresApproval,omitempty"`
}

// OptimizationActionStatus defines the observed state of an OptimizationAction.
type OptimizationActionStatus struct {
	// Result is the current result of the action.
	// +optional
	// +kubebuilder:validation:Enum=pending;approved;applied;failed;rolled_back
	Result ActionResult `json:"result,omitempty"`

	// AppliedAt is the timestamp when the action was applied.
	// +optional
	AppliedAt *metav1.Time `json:"appliedAt,omitempty"`

	// ApprovedBy is the identity of the user who approved the action.
	// +optional
	ApprovedBy string `json:"approvedBy,omitempty"`

	// ActualSavings is the realized savings in dollars after the action was applied.
	// +optional
	ActualSavings float64 `json:"actualSavings,omitempty"`

	// Message provides additional information about the action status.
	// +optional
	Message string `json:"message,omitempty"`

	// RollbackReason describes why the action was rolled back, if applicable.
	// +optional
	RollbackReason string `json:"rollbackReason,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=oa
// +kubebuilder:printcolumn:name="Type",type=string,JSONPath=`.spec.actionType`
// +kubebuilder:printcolumn:name="Policy",type=string,JSONPath=`.spec.policyRef`
// +kubebuilder:printcolumn:name="Savings",type=number,JSONPath=`.spec.expectedSavings`
// +kubebuilder:printcolumn:name="Result",type=string,JSONPath=`.status.result`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// OptimizationAction is the Schema for the optimizationactions API.
type OptimizationAction struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   OptimizationActionSpec   `json:"spec,omitempty"`
	Status OptimizationActionStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// OptimizationActionList contains a list of OptimizationAction.
type OptimizationActionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []OptimizationAction `json:"items"`
}

func init() {
	SchemeBuilder.Register(&OptimizationAction{}, &OptimizationActionList{})
}
