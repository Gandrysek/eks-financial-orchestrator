package api

import (
	"context"
	"time"

	"github.com/eks-financial-orchestrator/pkg/collector"
	"github.com/eks-financial-orchestrator/pkg/forecast"
)

// CostFilters defines optional filters for cost queries.
type CostFilters struct {
	Namespace string    `json:"namespace,omitempty"`
	Team      string    `json:"team,omitempty"`
	StartTime time.Time `json:"start_time,omitempty"`
	EndTime   time.Time `json:"end_time,omitempty"`
}

// CostResponse holds the response for cost data queries.
type CostResponse struct {
	Timestamp   time.Time                            `json:"timestamp"`
	TotalCost   float64                              `json:"total_cost"`
	ByNamespace map[string]*collector.NamespaceCost   `json:"by_namespace,omitempty"`
	ByTeam      map[string]*collector.TeamCost        `json:"by_team,omitempty"`
}

// ForecastResponse holds the response for forecast queries.
type ForecastResponse struct {
	GeneratedAt time.Time               `json:"generated_at"`
	Namespace   string                  `json:"namespace,omitempty"`
	Periods     []forecast.ForecastPeriod `json:"periods"`
}

// ReportParams defines parameters for generating a cost report.
type ReportParams struct {
	StartTime  time.Time `json:"start_time"`
	EndTime    time.Time `json:"end_time"`
	Namespaces []string  `json:"namespaces,omitempty"`
	Teams      []string  `json:"teams,omitempty"`
}

// CostReport holds a generated cost report.
type CostReport struct {
	GeneratedAt time.Time                            `json:"generated_at"`
	StartTime   time.Time                            `json:"start_time"`
	EndTime     time.Time                            `json:"end_time"`
	TotalCost   float64                              `json:"total_cost"`
	ByNamespace map[string]*collector.NamespaceCost   `json:"by_namespace,omitempty"`
	ByTeam      map[string]*collector.TeamCost        `json:"by_team,omitempty"`
}

// PolicyStatusResponse holds the status of all financial policies.
type PolicyStatusResponse struct {
	Policies []PolicyStatusEntry `json:"policies"`
}

// PolicyStatusEntry holds the status of a single financial policy.
type PolicyStatusEntry struct {
	Name               string    `json:"name"`
	Namespace          string    `json:"namespace"`
	Phase              string    `json:"phase"`
	CurrentCost        float64   `json:"current_cost"`
	BudgetLimit        float64   `json:"budget_limit"`
	BudgetUsagePercent float64   `json:"budget_usage_percent"`
	Mode               string    `json:"mode"`
	LastEvaluated      time.Time `json:"last_evaluated"`
}

// APIServer defines the REST API interface.
type APIServer interface {
	// GetCurrentCosts returns current cost data with optional filters.
	GetCurrentCosts(ctx context.Context, filters CostFilters) (*CostResponse, error)

	// GetForecast returns cost forecasts for the specified period.
	GetForecast(ctx context.Context, period forecast.ForecastPeriod) (*ForecastResponse, error)

	// GenerateReport creates a cost report for the specified time range
	// with breakdown by namespaces and services.
	GenerateReport(ctx context.Context, params ReportParams) (*CostReport, error)

	// GetPolicyStatus returns the current status of all financial policies.
	GetPolicyStatus(ctx context.Context) (*PolicyStatusResponse, error)
}
