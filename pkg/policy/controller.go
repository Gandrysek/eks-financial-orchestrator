package policy

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/eks-financial-orchestrator/api/v1alpha1"
)

// PolicyReconciler reconciles FinancialPolicy objects.
type PolicyReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Logger logr.Logger
}

// Reconcile handles create/update/delete events for FinancialPolicy CRDs.
// It validates the policy and updates the status accordingly.
func (r *PolicyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Logger.WithValues("financialpolicy", req.NamespacedName)

	// Get the FinancialPolicy CR.
	var policy v1alpha1.FinancialPolicy
	if err := r.Get(ctx, req.NamespacedName, &policy); err != nil {
		if errors.IsNotFound(err) {
			log.Info("FinancialPolicy resource not found, likely deleted")
			return ctrl.Result{}, nil
		}
		log.Error(err, "unable to fetch FinancialPolicy")
		return ctrl.Result{}, err
	}

	// Validate the policy.
	validationErrs := ValidateFinancialPolicy(&policy)

	now := metav1.Now()

	if len(validationErrs) > 0 {
		msgs := make([]string, len(validationErrs))
		for i, ve := range validationErrs {
			msgs[i] = fmt.Sprintf("%s: %s", ve.Field, ve.Message)
		}
		errMsg := strings.Join(msgs, "; ")

		policy.Status.Phase = v1alpha1.PolicyPhaseError
		setCondition(&policy, "Valid", "False", "ValidationFailed", errMsg, now)

		log.Info("FinancialPolicy validation failed", "errors", errMsg)
	} else {
		policy.Status.Phase = v1alpha1.PolicyPhaseActive
		setCondition(&policy, "Valid", "True", "ValidationPassed", "Policy is valid", now)

		log.Info("FinancialPolicy validated successfully")
	}

	// Update the status subresource.
	if err := r.Status().Update(ctx, &policy); err != nil {
		log.Error(err, "unable to update FinancialPolicy status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// setCondition sets or updates a condition on the FinancialPolicy status.
func setCondition(policy *v1alpha1.FinancialPolicy, condType, status, reason, message string, now metav1.Time) {
	for i, c := range policy.Status.Conditions {
		if c.Type == condType {
			policy.Status.Conditions[i].Status = status
			policy.Status.Conditions[i].Reason = reason
			policy.Status.Conditions[i].Message = message
			policy.Status.Conditions[i].LastTransitionTime = now
			return
		}
	}
	policy.Status.Conditions = append(policy.Status.Conditions, v1alpha1.PolicyCondition{
		Type:               condType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: now,
	})
}

// SetupWithManager registers the PolicyReconciler with the controller manager.
func (r *PolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.FinancialPolicy{}).
		Complete(r)
}
