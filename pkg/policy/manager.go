package policy

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/eks-financial-orchestrator/api/v1alpha1"
)

const (
	// VersionHistoryAnnotation is the annotation key for storing policy version history.
	VersionHistoryAnnotation = "finops.eks.io/version-history"

	// maxVersionHistory is the maximum number of versions to keep in the annotation.
	maxVersionHistory = 10
)

// DefaultPolicyManager implements the PolicyManager interface using Kubernetes CRDs.
type DefaultPolicyManager struct {
	client client.Client
	logger logr.Logger
}

// NewDefaultPolicyManager creates a new DefaultPolicyManager.
func NewDefaultPolicyManager(client client.Client, logger logr.Logger) *DefaultPolicyManager {
	return &DefaultPolicyManager{
		client: client,
		logger: logger,
	}
}

// ValidatePolicy checks a FinancialPolicy against validation rules.
// Returns validation errors if the policy is invalid.
func (m *DefaultPolicyManager) ValidatePolicy(ctx context.Context, policy *v1alpha1.FinancialPolicy) ([]ValidationError, error) {
	errs := ValidateFinancialPolicy(policy)
	return errs, nil
}

// ApplyPolicy stores or updates a FinancialPolicy as a Kubernetes CR.
// It validates the policy first, auto-increments the version on updates,
// and stores version history in annotations for rollback support.
func (m *DefaultPolicyManager) ApplyPolicy(ctx context.Context, policy *v1alpha1.FinancialPolicy) error {
	// Validate the policy first.
	validationErrs := ValidateFinancialPolicy(policy)
	if len(validationErrs) > 0 {
		msgs := make([]string, len(validationErrs))
		for i, ve := range validationErrs {
			msgs[i] = fmt.Sprintf("%s: %s", ve.Field, ve.Message)
		}
		return fmt.Errorf("policy validation failed: %s", strings.Join(msgs, "; "))
	}

	// Try to get the existing policy.
	existing := &v1alpha1.FinancialPolicy{}
	err := m.client.Get(ctx, types.NamespacedName{
		Name:      policy.Name,
		Namespace: policy.Namespace,
	}, existing)

	if err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("failed to get existing policy: %w", err)
		}

		// Policy does not exist — create it.
		policy.Spec.Version = 1
		if err := initVersionHistory(policy); err != nil {
			return fmt.Errorf("failed to initialize version history: %w", err)
		}

		m.logger.Info("Creating new financial policy",
			"name", policy.Name,
			"namespace", policy.Namespace,
			"version", policy.Spec.Version,
		)
		if err := m.client.Create(ctx, policy); err != nil {
			return fmt.Errorf("failed to create policy: %w", err)
		}
		return nil
	}

	// Policy exists — update it.
	// Store the previous spec in version history before applying the new spec.
	if err := appendVersionHistory(existing); err != nil {
		return fmt.Errorf("failed to append version history: %w", err)
	}

	// Auto-increment version.
	newVersion := existing.Spec.Version + 1
	policy.Spec.Version = newVersion

	// Copy version history annotations to the updated policy.
	if policy.Annotations == nil {
		policy.Annotations = make(map[string]string)
	}
	if existing.Annotations != nil {
		policy.Annotations[VersionHistoryAnnotation] = existing.Annotations[VersionHistoryAnnotation]
	}

	// Preserve the resource version for the update.
	policy.ResourceVersion = existing.ResourceVersion

	m.logger.Info("Updating financial policy",
		"name", policy.Name,
		"namespace", policy.Namespace,
		"version", newVersion,
	)
	if err := m.client.Update(ctx, policy); err != nil {
		return fmt.Errorf("failed to update policy: %w", err)
	}
	return nil
}

// GetActivePolicy retrieves the currently active policy for a namespace.
// Returns the first policy with status phase "Active", or the first policy found
// if none has an Active phase. Returns nil if no policies exist in the namespace.
func (m *DefaultPolicyManager) GetActivePolicy(ctx context.Context, namespace string) (*v1alpha1.FinancialPolicy, error) {
	list := &v1alpha1.FinancialPolicyList{}
	if err := m.client.List(ctx, list, client.InNamespace(namespace)); err != nil {
		return nil, fmt.Errorf("failed to list policies in namespace %s: %w", namespace, err)
	}

	if len(list.Items) == 0 {
		return nil, nil
	}

	// Return the first policy with Active phase.
	for i := range list.Items {
		if list.Items[i].Status.Phase == v1alpha1.PolicyPhaseActive {
			return &list.Items[i], nil
		}
	}

	// No Active policy found — return the first one.
	return &list.Items[0], nil
}

// ListPolicies returns all financial policies across all namespaces.
func (m *DefaultPolicyManager) ListPolicies(ctx context.Context) ([]*v1alpha1.FinancialPolicy, error) {
	list := &v1alpha1.FinancialPolicyList{}
	if err := m.client.List(ctx, list); err != nil {
		return nil, fmt.Errorf("failed to list policies: %w", err)
	}

	result := make([]*v1alpha1.FinancialPolicy, len(list.Items))
	for i := range list.Items {
		result[i] = &list.Items[i]
	}
	return result, nil
}

// findPolicyByName searches all namespaces for a FinancialPolicy with the given name.
// Returns the first match or an error if not found.
func (m *DefaultPolicyManager) findPolicyByName(ctx context.Context, name string) (*v1alpha1.FinancialPolicy, error) {
	policies, err := m.ListPolicies(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list policies: %w", err)
	}
	for _, p := range policies {
		if p.Name == name {
			return p, nil
		}
	}
	return nil, fmt.Errorf("policy %q not found", name)
}

// parseVersionHistory reads and parses the version history annotation from a policy.
func parseVersionHistory(policy *v1alpha1.FinancialPolicy) ([]PolicyVersion, error) {
	if policy.Annotations == nil {
		return nil, nil
	}
	raw, ok := policy.Annotations[VersionHistoryAnnotation]
	if !ok || raw == "" {
		return nil, nil
	}
	var history []PolicyVersion
	if err := json.Unmarshal([]byte(raw), &history); err != nil {
		return nil, fmt.Errorf("failed to unmarshal version history: %w", err)
	}
	return history, nil
}

// GetPolicyHistory returns the version history of a policy, including the current
// spec as the latest entry. The result is sorted by version number ascending.
func (m *DefaultPolicyManager) GetPolicyHistory(ctx context.Context, name string) ([]PolicyVersion, error) {
	policy, err := m.findPolicyByName(ctx, name)
	if err != nil {
		return nil, err
	}

	history, err := parseVersionHistory(policy)
	if err != nil {
		return nil, err
	}

	// Append the current spec as the latest version entry.
	current := PolicyVersion{
		Version:   policy.Spec.Version,
		Spec:      *policy.Spec.DeepCopy(),
		Timestamp: time.Now(),
	}
	history = append(history, current)

	// Sort by version ascending.
	sort.Slice(history, func(i, j int) bool {
		return history[i].Version < history[j].Version
	})

	return history, nil
}

// RollbackPolicy reverts a policy to a specified previous version.
// It finds the target version in the history, saves the current spec to history,
// replaces the spec with the target version's spec, increments the version number,
// and sets the status phase to "RolledBack".
func (m *DefaultPolicyManager) RollbackPolicy(ctx context.Context, name string, version int) error {
	policy, err := m.findPolicyByName(ctx, name)
	if err != nil {
		return err
	}

	history, err := parseVersionHistory(policy)
	if err != nil {
		return err
	}

	// Find the target version in the history.
	var targetSpec *v1alpha1.FinancialPolicySpec
	for i := range history {
		if history[i].Version == version {
			targetSpec = history[i].Spec.DeepCopy()
			break
		}
	}
	if targetSpec == nil {
		return fmt.Errorf("version %d not found in history for policy %q", version, name)
	}

	// Append the current spec to history before applying the rollback.
	if err := appendVersionHistory(policy); err != nil {
		return fmt.Errorf("failed to append current version to history: %w", err)
	}

	// Increment the version number (rollback creates a new version).
	newVersion := policy.Spec.Version + 1

	// Replace the spec with the target version's spec.
	policy.Spec = *targetSpec
	policy.Spec.Version = newVersion

	// Set status phase to RolledBack.
	policy.Status.Phase = v1alpha1.PolicyPhaseRolledBack

	m.logger.Info("Rolling back financial policy",
		"name", policy.Name,
		"namespace", policy.Namespace,
		"fromVersion", newVersion-1,
		"toVersion", version,
		"newVersion", newVersion,
	)

	if err := m.client.Update(ctx, policy); err != nil {
		return fmt.Errorf("failed to update policy during rollback: %w", err)
	}

	// Update the status subresource.
	if err := m.client.Status().Update(ctx, policy); err != nil {
		return fmt.Errorf("failed to update policy status during rollback: %w", err)
	}

	return nil
}

// initVersionHistory initializes the version history annotation on a new policy.
func initVersionHistory(policy *v1alpha1.FinancialPolicy) error {
	if policy.Annotations == nil {
		policy.Annotations = make(map[string]string)
	}

	history := []PolicyVersion{}
	data, err := json.Marshal(history)
	if err != nil {
		return fmt.Errorf("failed to marshal empty version history: %w", err)
	}
	policy.Annotations[VersionHistoryAnnotation] = string(data)
	return nil
}

// appendVersionHistory appends the current spec of the policy to its version history annotation.
// It limits the history to the last maxVersionHistory entries.
func appendVersionHistory(policy *v1alpha1.FinancialPolicy) error {
	if policy.Annotations == nil {
		policy.Annotations = make(map[string]string)
	}

	var history []PolicyVersion
	if raw, ok := policy.Annotations[VersionHistoryAnnotation]; ok && raw != "" {
		if err := json.Unmarshal([]byte(raw), &history); err != nil {
			return fmt.Errorf("failed to unmarshal version history: %w", err)
		}
	}

	// Append the current spec as a version entry.
	entry := PolicyVersion{
		Version:   policy.Spec.Version,
		Spec:      *policy.Spec.DeepCopy(),
		Timestamp: time.Now(),
	}
	history = append(history, entry)

	// Trim to the last maxVersionHistory entries.
	if len(history) > maxVersionHistory {
		history = history[len(history)-maxVersionHistory:]
	}

	data, err := json.Marshal(history)
	if err != nil {
		return fmt.Errorf("failed to marshal version history: %w", err)
	}
	policy.Annotations[VersionHistoryAnnotation] = string(data)
	return nil
}
