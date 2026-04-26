package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CostReportSpec defines the desired state of a CostReport.
type CostReportSpec struct {
	// StartTime is the beginning of the reporting period.
	// +kubebuilder:validation:Required
	StartTime metav1.Time `json:"startTime"`

	// EndTime is the end of the reporting period.
	// +kubebuilder:validation:Required
	EndTime metav1.Time `json:"endTime"`

	// Namespaces is the list of namespaces to include in the report.
	// If empty, all namespaces are included.
	// +optional
	Namespaces []string `json:"namespaces,omitempty"`

	// Teams is the list of teams to include in the report.
	// If empty, all teams are included.
	// +optional
	Teams []string `json:"teams,omitempty"`
}

// NamespaceCostSummary holds cost data for a single namespace in a report.
type NamespaceCostSummary struct {
	// Namespace is the Kubernetes namespace name.
	Namespace string `json:"namespace"`

	// DirectCost is the cost directly attributable to the namespace.
	DirectCost float64 `json:"directCost"`

	// SharedCost is the namespace's share of shared cluster costs.
	SharedCost float64 `json:"sharedCost"`

	// TotalCost is the sum of direct and shared costs.
	TotalCost float64 `json:"totalCost"`

	// ByService breaks down costs by service within the namespace.
	// +optional
	ByService map[string]float64 `json:"byService,omitempty"`
}

// TeamCostSummary holds cost data for a single team in a report.
type TeamCostSummary struct {
	// Team is the team name.
	Team string `json:"team"`

	// DirectCost is the cost of pods owned by the team.
	DirectCost float64 `json:"directCost"`

	// IndirectCost is the team's share of shared resources.
	IndirectCost float64 `json:"indirectCost"`

	// TotalCost is the sum of direct and indirect costs.
	TotalCost float64 `json:"totalCost"`
}

// CostReportStatus defines the observed state of a CostReport.
type CostReportStatus struct {
	// Phase is the current phase of the report generation.
	// +optional
	// +kubebuilder:validation:Enum=Pending;Generating;Ready;Error
	Phase string `json:"phase,omitempty"`

	// TotalCost is the total cost for the reporting period.
	// +optional
	TotalCost float64 `json:"totalCost,omitempty"`

	// ByNamespace contains cost breakdowns per namespace.
	// +optional
	ByNamespace []NamespaceCostSummary `json:"byNamespace,omitempty"`

	// ByTeam contains cost breakdowns per team.
	// +optional
	ByTeam []TeamCostSummary `json:"byTeam,omitempty"`

	// GeneratedAt is the timestamp when the report was generated.
	// +optional
	GeneratedAt *metav1.Time `json:"generatedAt,omitempty"`

	// Message provides additional information about the report status.
	// +optional
	Message string `json:"message,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=cr
// +kubebuilder:printcolumn:name="Start",type=string,JSONPath=`.spec.startTime`
// +kubebuilder:printcolumn:name="End",type=string,JSONPath=`.spec.endTime`
// +kubebuilder:printcolumn:name="TotalCost",type=number,JSONPath=`.status.totalCost`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// CostReport is the Schema for the costreports API.
type CostReport struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CostReportSpec   `json:"spec,omitempty"`
	Status CostReportStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// CostReportList contains a list of CostReport.
type CostReportList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CostReport `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CostReport{}, &CostReportList{})
}
