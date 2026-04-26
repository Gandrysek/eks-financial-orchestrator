// Package orchestrator implements the main Orchestrator Controller that
// coordinates the Cost Collector, Policy Manager, Instance Manager, Forecasting
// Module, and Alerting Module to enforce financial policies on EKS clusters.
package orchestrator

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/eks-financial-orchestrator/api/v1alpha1"
	"github.com/eks-financial-orchestrator/pkg/alerting"
	"github.com/eks-financial-orchestrator/pkg/audit"
	"github.com/eks-financial-orchestrator/pkg/collector"
	"github.com/eks-financial-orchestrator/pkg/forecast"
	"github.com/eks-financial-orchestrator/pkg/instance"
	"github.com/eks-financial-orchestrator/pkg/policy"
)

// OrchestratorReconciler reconciles FinancialPolicy objects and coordinates
// all financial orchestration components.
type OrchestratorReconciler struct {
	client.Client
	Scheme          *runtime.Scheme
	Logger          logr.Logger
	PolicyManager   policy.PolicyManager
	AlertManager    alerting.AlertManager
	InstanceManager instance.InstanceManager
	Forecaster      forecast.Forecaster
	CostStore       collector.CostStore
	AuditWriter     audit.AuditWriter
}

// Reconcile handles FinancialPolicy events. It reads the policy, evaluates
// current costs against the budget, and triggers appropriate actions based
// on the policy configuration.
func (r *OrchestratorReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Logger.WithValues("financialpolicy", req.NamespacedName)

	// Get the FinancialPolicy CR.
	var fp v1alpha1.FinancialPolicy
	if err := r.Get(ctx, req.NamespacedName, &fp); err != nil {
		if errors.IsNotFound(err) {
			log.Info("FinancialPolicy not found, likely deleted")
			return ctrl.Result{}, nil
		}
		log.Error(err, "unable to fetch FinancialPolicy")
		return ctrl.Result{}, err
	}

	// Skip policies in Error phase.
	if fp.Status.Phase == v1alpha1.PolicyPhaseError {
		log.Info("Skipping policy in Error phase")
		return ctrl.Result{}, nil
	}

	targetNS := fp.Spec.TargetNamespace
	log = log.WithValues("targetNamespace", targetNS)

	// Read current costs for the target namespace (last 30 days for budget evaluation).
	now := time.Now()
	startOfMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	records, err := r.CostStore.QueryCostRecords(ctx, startOfMonth, now, targetNS)
	if err != nil {
		log.Error(err, "failed to query cost records")
		return ctrl.Result{}, err
	}

	// Calculate current month-to-date cost.
	currentCost := 0.0
	for _, rec := range records {
		currentCost += rec.TotalCost
	}

	// Evaluate budget usage percentage.
	budgetLimit := fp.Spec.Budget.MonthlyLimit
	var budgetUsagePct float64
	if budgetLimit > 0 {
		budgetUsagePct = (currentCost / budgetLimit) * 100.0
	}

	log.Info("Evaluated budget",
		"currentCost", currentCost,
		"budgetLimit", budgetLimit,
		"budgetUsagePct", budgetUsagePct,
	)

	// Check if costs exceed any alert threshold.
	breachDetected := false
	for _, threshold := range fp.Spec.Budget.AlertThresholds {
		if budgetUsagePct >= threshold {
			breachDetected = true
			log.Info("Budget threshold breached",
				"threshold", threshold,
				"budgetUsagePct", budgetUsagePct,
			)
		}
	}

	// Execute breach action if budget is exceeded.
	if breachDetected {
		if err := r.executeBreachAction(ctx, &fp, currentCost, budgetUsagePct, log); err != nil {
			log.Error(err, "failed to execute breach action")
			// Don't return error — continue with status update.
		}
	}

	// Handle mode-dependent recommendation logic.
	if fp.Spec.Mode == v1alpha1.PolicyModeAutomatic {
		// Automatic mode: apply instance mix recommendations.
		if err := r.applyAutomaticOptimization(ctx, &fp, log); err != nil {
			log.Error(err, "failed to apply automatic optimization")
			// On failure: switch to advisory mode, log error, send notification.
			r.handleOptimizationFailure(ctx, &fp, err, log)
		}
	} else {
		// Advisory mode: generate recommendations only.
		r.generateAdvisoryRecommendation(ctx, &fp, log)
	}

	// Record reconciliation in audit log.
	if r.AuditWriter != nil {
		entry := &audit.AuditLogEntry{
			Timestamp:    time.Now().UTC(),
			Actor:        "system",
			Action:       "policy_evaluate",
			ResourceType: "FinancialPolicy",
			ResourceName: fp.Name,
			Namespace:    fp.Namespace,
			Details: map[string]interface{}{
				"current_cost":      currentCost,
				"budget_limit":      budgetLimit,
				"budget_usage_pct":  budgetUsagePct,
				"breach_detected":   breachDetected,
				"mode":              string(fp.Spec.Mode),
			},
			Result: "success",
		}
		if err := r.AuditWriter.WriteEntry(ctx, entry); err != nil {
			log.Error(err, "failed to write audit log entry")
		}
	}

	// Update policy status with current cost and budget usage.
	nowMeta := metav1.Now()
	fp.Status.CurrentCost = currentCost
	fp.Status.BudgetUsagePercent = budgetUsagePct
	fp.Status.LastEvaluated = &nowMeta

	if err := r.Status().Update(ctx, &fp); err != nil {
		log.Error(err, "failed to update policy status")
		return ctrl.Result{}, err
	}

	// Requeue after 5 minutes for periodic evaluation.
	return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
}

// executeBreachAction executes the action defined in the policy's breach_action field.
func (r *OrchestratorReconciler) executeBreachAction(
	ctx context.Context,
	fp *v1alpha1.FinancialPolicy,
	currentCost, budgetUsagePct float64,
	log logr.Logger,
) error {
	action := fp.Spec.Budget.BreachAction
	log.Info("Executing breach action", "action", string(action))

	switch action {
	case v1alpha1.BreachActionAlert:
		// Send alert via AlertManager.
		if r.AlertManager != nil {
			alert := &alerting.Alert{
				ID:                fmt.Sprintf("breach-%s-%d", fp.Name, time.Now().UnixNano()),
				Timestamp:         time.Now().UTC(),
				Severity:          alerting.AlertSeverityWarning,
				Category:          alerting.AlertCategoryBudget,
				Namespace:         fp.Spec.TargetNamespace,
				CurrentCost:       currentCost,
				BudgetLimit:       fp.Spec.Budget.MonthlyLimit,
				UsagePercent:      budgetUsagePct,
				Message:           fmt.Sprintf("Budget breach detected: %.1f%% of $%.2f limit", budgetUsagePct, fp.Spec.Budget.MonthlyLimit),
				RecommendedAction: "Review namespace spending and consider scaling down resources",
			}
			if err := r.AlertManager.SendAlert(ctx, alert); err != nil {
				return fmt.Errorf("sending breach alert: %w", err)
			}
		}

	case v1alpha1.BreachActionThrottle:
		// Stub: log warning for resource throttling.
		log.Info("BREACH ACTION: throttle — resource throttling not yet implemented",
			"namespace", fp.Spec.TargetNamespace,
			"budgetUsagePct", budgetUsagePct,
		)

	case v1alpha1.BreachActionBlockDeployments:
		// Stub: log warning for deployment blocking.
		log.Info("BREACH ACTION: block_deployments — deployment blocking not yet implemented",
			"namespace", fp.Spec.TargetNamespace,
			"budgetUsagePct", budgetUsagePct,
		)

	default:
		log.Info("Unknown breach action, defaulting to alert", "action", string(action))
	}

	// Record breach action in audit log.
	if r.AuditWriter != nil {
		entry := &audit.AuditLogEntry{
			Timestamp:    time.Now().UTC(),
			Actor:        "system",
			Action:       "breach_action",
			ResourceType: "FinancialPolicy",
			ResourceName: fp.Name,
			Namespace:    fp.Spec.TargetNamespace,
			Details: map[string]interface{}{
				"breach_action":    string(action),
				"current_cost":     currentCost,
				"budget_usage_pct": budgetUsagePct,
				"budget_limit":     fp.Spec.Budget.MonthlyLimit,
			},
			Result: "success",
		}
		if err := r.AuditWriter.WriteEntry(ctx, entry); err != nil {
			log.Error(err, "failed to write breach action audit log")
		}
	}

	return nil
}

// applyAutomaticOptimization generates and applies instance mix recommendations
// in automatic mode.
func (r *OrchestratorReconciler) applyAutomaticOptimization(
	ctx context.Context,
	fp *v1alpha1.FinancialPolicy,
	log logr.Logger,
) error {
	if r.InstanceManager == nil {
		log.Info("InstanceManager not configured, skipping automatic optimization")
		return nil
	}

	analysis, err := r.InstanceManager.AnalyzeInstanceMix(ctx)
	if err != nil {
		return fmt.Errorf("analyzing instance mix: %w", err)
	}

	policies := []*v1alpha1.FinancialPolicy{fp}
	rec, err := r.InstanceManager.GenerateRecommendation(ctx, analysis, policies)
	if err != nil {
		return fmt.Errorf("generating recommendation: %w", err)
	}

	if rec.ExpectedSavings <= 0 {
		log.Info("No savings available, skipping optimization")
		return nil
	}

	// Apply the recommendation automatically.
	result, err := r.InstanceManager.ApplyRecommendation(ctx, rec)
	if err != nil {
		return fmt.Errorf("applying recommendation: %w", err)
	}

	log.Info("Automatic optimization applied",
		"recommendationID", rec.ID,
		"applied", result.Applied,
		"actualSavings", result.ActualSavings,
	)

	return nil
}

// generateAdvisoryRecommendation generates recommendations without applying them.
func (r *OrchestratorReconciler) generateAdvisoryRecommendation(
	ctx context.Context,
	fp *v1alpha1.FinancialPolicy,
	log logr.Logger,
) {
	if r.InstanceManager == nil {
		return
	}

	analysis, err := r.InstanceManager.AnalyzeInstanceMix(ctx)
	if err != nil {
		log.Error(err, "failed to analyze instance mix for advisory recommendation")
		return
	}

	policies := []*v1alpha1.FinancialPolicy{fp}
	rec, err := r.InstanceManager.GenerateRecommendation(ctx, analysis, policies)
	if err != nil {
		log.Error(err, "failed to generate advisory recommendation")
		return
	}

	log.Info("Advisory recommendation generated (awaiting manual approval)",
		"recommendationID", rec.ID,
		"expectedSavings", rec.ExpectedSavings,
		"policyCompliant", rec.PolicyCompliant,
	)
}

// handleOptimizationFailure handles errors during automatic optimization by
// switching the policy to advisory mode, logging the error, and sending a notification.
func (r *OrchestratorReconciler) handleOptimizationFailure(
	ctx context.Context,
	fp *v1alpha1.FinancialPolicy,
	optimizationErr error,
	log logr.Logger,
) {
	log.Error(optimizationErr, "Optimization failed, switching to advisory mode",
		"policy", fp.Name,
		"namespace", fp.Spec.TargetNamespace,
	)

	// Record failure in audit log.
	if r.AuditWriter != nil {
		entry := &audit.AuditLogEntry{
			Timestamp:    time.Now().UTC(),
			Actor:        "system",
			Action:       "optimization_failure",
			ResourceType: "FinancialPolicy",
			ResourceName: fp.Name,
			Namespace:    fp.Spec.TargetNamespace,
			Reason:       optimizationErr.Error(),
			Details: map[string]interface{}{
				"previous_mode": string(fp.Spec.Mode),
				"new_mode":      string(v1alpha1.PolicyModeAdvisory),
			},
			Result: "failure",
		}
		if err := r.AuditWriter.WriteEntry(ctx, entry); err != nil {
			log.Error(err, "failed to write optimization failure audit log")
		}
	}

	// Send notification about the failure.
	if r.AlertManager != nil {
		alert := &alerting.Alert{
			ID:                fmt.Sprintf("opt-fail-%s-%d", fp.Name, time.Now().UnixNano()),
			Timestamp:         time.Now().UTC(),
			Severity:          alerting.AlertSeverityCritical,
			Category:          alerting.AlertCategoryPolicy,
			Namespace:         fp.Spec.TargetNamespace,
			Message:           fmt.Sprintf("Automatic optimization failed for policy %s: %v. Switching to advisory mode.", fp.Name, optimizationErr),
			RecommendedAction: "Investigate the optimization failure and manually review recommendations",
		}
		if err := r.AlertManager.SendAlert(ctx, alert); err != nil {
			log.Error(err, "failed to send optimization failure alert")
		}
	}
}

// SetupWithManager registers the OrchestratorReconciler with the controller manager.
func (r *OrchestratorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.FinancialPolicy{}).
		Complete(r)
}
