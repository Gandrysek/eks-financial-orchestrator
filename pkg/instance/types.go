package instance

import (
	"context"
	"time"

	v1alpha1 "github.com/eks-financial-orchestrator/api/v1alpha1"
)

// InstanceMixAnalysis represents the current state of the instance mix.
type InstanceMixAnalysis struct {
	Timestamp        time.Time                         `json:"timestamp"`
	TotalNodes       int                               `json:"total_nodes"`
	ByPurchaseOption map[string]*PurchaseOptionStats    `json:"by_purchase_option"`
	CurrentCost      float64                           `json:"current_cost"`
	OptimalCost      float64                           `json:"optimal_cost"`
	PotentialSavings float64                           `json:"potential_savings"`
}

// PurchaseOptionStats holds statistics for a single purchase option category.
type PurchaseOptionStats struct {
	Count      int     `json:"count"`
	Percentage float64 `json:"percentage"`
	Cost       float64 `json:"cost"`
}

// OptimizationRecommendation represents a suggested instance mix change.
type OptimizationRecommendation struct {
	ID              string             `json:"id"`
	Timestamp       time.Time          `json:"timestamp"`
	CurrentMix      map[string]float64 `json:"current_mix"`
	RecommendedMix  map[string]float64 `json:"recommended_mix"`
	ExpectedSavings float64            `json:"expected_savings"`
	Reason          string             `json:"reason"`
	AffectedNodes   []string           `json:"affected_nodes"`
	PolicyCompliant bool               `json:"policy_compliant"`
}

// OptimizationResult holds the outcome of applying an optimization recommendation.
type OptimizationResult struct {
	RecommendationID string    `json:"recommendation_id"`
	Applied          bool      `json:"applied"`
	Timestamp        time.Time `json:"timestamp"`
	ActualSavings    float64   `json:"actual_savings"`
	Message          string    `json:"message"`
}

// SavingsPlanRecommendation holds a recommendation for Savings Plan or
// Reserved Instance purchases based on historical usage analysis.
type SavingsPlanRecommendation struct {
	Timestamp         time.Time `json:"timestamp"`
	AnalysisPeriodDays int      `json:"analysis_period_days"`
	CurrentMonthlyCost float64  `json:"current_monthly_cost"`
	EstimatedSavings   float64  `json:"estimated_savings"`
	RecommendedPlan    string   `json:"recommended_plan"` // savings_plan or reserved_instance
	CommitmentAmount   float64  `json:"commitment_amount"`
	CoveragePercent    float64  `json:"coverage_percent"`
}

// InstanceManager defines the interface for instance mix optimization.
type InstanceManager interface {
	// AnalyzeInstanceMix evaluates the current Spot/OnDemand/Reserved ratio.
	AnalyzeInstanceMix(ctx context.Context) (*InstanceMixAnalysis, error)

	// GenerateRecommendation produces an optimization recommendation
	// respecting active financial policy constraints.
	GenerateRecommendation(ctx context.Context, analysis *InstanceMixAnalysis, policies []*v1alpha1.FinancialPolicy) (*OptimizationRecommendation, error)

	// ApplyRecommendation executes an optimization recommendation.
	ApplyRecommendation(ctx context.Context, rec *OptimizationRecommendation) (*OptimizationResult, error)

	// HandleSpotInterruption processes a Spot interruption notice
	// and migrates workloads to available nodes.
	HandleSpotInterruption(ctx context.Context, nodeID string) error

	// EvaluateSavingsPlans analyzes 30 days of usage data and recommends
	// Savings Plan or Reserved Instance purchases.
	EvaluateSavingsPlans(ctx context.Context) (*SavingsPlanRecommendation, error)
}
