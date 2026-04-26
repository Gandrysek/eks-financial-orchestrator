// Package metrics implements the Prometheus exporter for the EKS Financial
// Orchestrator, registering and exposing financial metrics such as cost per
// namespace, budget usage, forecasted costs, and optimization savings.
package metrics

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/eks-financial-orchestrator/pkg/collector"
	"github.com/eks-financial-orchestrator/pkg/forecast"
)

var (
	// CostPerNamespace tracks cost in dollars per namespace.
	CostPerNamespace = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "eks_fo_cost_per_namespace_dollars",
			Help: "Current cost in dollars per namespace",
		},
		[]string{"namespace", "team"},
	)

	// CostPerInstanceType tracks cost in dollars per instance type.
	CostPerInstanceType = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "eks_fo_cost_per_instance_type_dollars",
			Help: "Current cost in dollars per instance type",
		},
		[]string{"instance_type", "purchase_option"},
	)

	// BudgetUsagePercent tracks budget usage percentage per namespace.
	BudgetUsagePercent = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "eks_fo_budget_usage_percent",
			Help: "Budget usage percentage per namespace",
		},
		[]string{"namespace", "policy_name"},
	)

	// ForecastedCost tracks forecasted cost in dollars.
	ForecastedCost = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "eks_fo_forecasted_cost_dollars",
			Help: "Forecasted cost in dollars",
		},
		[]string{"period", "bound"},
	)

	// OptimizationSavings tracks total savings from optimization actions.
	OptimizationSavings = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "eks_fo_optimization_savings_dollars",
			Help: "Total savings from optimization actions in dollars",
		},
	)

	// AWSAuthErrors counts AWS authentication errors.
	AWSAuthErrors = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "eks_fo_aws_auth_errors_total",
			Help: "Total number of AWS authentication errors",
		},
	)

	// NotificationFailures counts notification delivery failures.
	NotificationFailures = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "eks_fo_notification_failures_total",
			Help: "Total number of notification delivery failures",
		},
	)
)

// RegisterMetrics registers all Prometheus metrics with the default registry.
func RegisterMetrics() {
	prometheus.MustRegister(
		CostPerNamespace,
		CostPerInstanceType,
		BudgetUsagePercent,
		ForecastedCost,
		OptimizationSavings,
		AWSAuthErrors,
		NotificationFailures,
	)
}

// UpdateCostMetrics updates the cost-related Prometheus metrics from aggregated costs.
func UpdateCostMetrics(costs *collector.AggregatedCosts) {
	if costs == nil {
		return
	}

	// Reset namespace cost metrics before updating.
	CostPerNamespace.Reset()

	for nsName, nsCost := range costs.ByNamespace {
		team := nsCost.Team
		if team == "" {
			team = "unallocated"
		}
		CostPerNamespace.WithLabelValues(nsName, team).Set(nsCost.TotalCost)
	}
}

// UpdateForecastMetrics updates the forecast-related Prometheus metrics.
func UpdateForecastMetrics(fc *forecast.Forecast) {
	if fc == nil {
		return
	}

	// Reset forecast metrics before updating.
	ForecastedCost.Reset()

	for _, period := range fc.Periods {
		periodLabel := fmt.Sprintf("%dd", period.Days)
		ForecastedCost.WithLabelValues(periodLabel, "forecasted").Set(period.ForecastedCost)
		ForecastedCost.WithLabelValues(periodLabel, "lower").Set(period.LowerBound)
		ForecastedCost.WithLabelValues(periodLabel, "upper").Set(period.UpperBound)
	}
}

// IncrementAWSAuthErrors increments the AWS authentication error counter.
func IncrementAWSAuthErrors() {
	AWSAuthErrors.Inc()
}

// IncrementNotificationFailures increments the notification failure counter.
func IncrementNotificationFailures() {
	NotificationFailures.Inc()
}
