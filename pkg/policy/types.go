package policy

import (
	"context"
	"time"

	v1alpha1 "github.com/eks-financial-orchestrator/api/v1alpha1"
)

// ValidationError describes a single validation failure for a FinancialPolicy.
type ValidationError struct {
	// Field is the JSON path of the field that failed validation.
	Field string
	// Message describes what is wrong with the field.
	Message string
}

// PolicyVersion represents a historical snapshot of a policy spec at a point in time.
type PolicyVersion struct {
	// Version is the policy version number.
	Version int
	// Spec is the policy spec at this version.
	Spec v1alpha1.FinancialPolicySpec
	// Timestamp is when this version was recorded.
	Timestamp time.Time
}

// PolicyManager defines the interface for financial policy management.
type PolicyManager interface {
	// ValidatePolicy checks a FinancialPolicy against the JSON schema.
	// Returns validation errors if the policy is invalid.
	ValidatePolicy(ctx context.Context, policy *v1alpha1.FinancialPolicy) ([]ValidationError, error)

	// ApplyPolicy stores or updates a FinancialPolicy as a Kubernetes CR.
	ApplyPolicy(ctx context.Context, policy *v1alpha1.FinancialPolicy) error

	// GetActivePolicy retrieves the currently active policy for a namespace.
	GetActivePolicy(ctx context.Context, namespace string) (*v1alpha1.FinancialPolicy, error)

	// ListPolicies returns all financial policies in the cluster.
	ListPolicies(ctx context.Context) ([]*v1alpha1.FinancialPolicy, error)

	// RollbackPolicy reverts a policy to a specified previous version.
	RollbackPolicy(ctx context.Context, name string, version int) error

	// GetPolicyHistory returns the version history of a policy.
	GetPolicyHistory(ctx context.Context, name string) ([]PolicyVersion, error)
}
