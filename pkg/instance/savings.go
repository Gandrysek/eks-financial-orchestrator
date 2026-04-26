package instance

import (
	"context"
	"fmt"
	"time"
)

// EvaluateSavingsPlans analyzes 30 days of historical cost data and
// recommends Savings Plan or Reserved Instance purchases.
func (m *DefaultInstanceManager) EvaluateSavingsPlans(ctx context.Context) (*SavingsPlanRecommendation, error) {
	end := time.Now().UTC()
	start := end.AddDate(0, 0, -30)

	var records []DailyCostRecord
	if m.costStore != nil {
		var err error
		records, err = m.costStore.GetDailyCosts(ctx, start, end)
		if err != nil {
			return nil, fmt.Errorf("reading historical cost data: %w", err)
		}
	}

	// Calculate average daily On-Demand cost from the records.
	totalCost := 0.0
	days := len(records)
	for _, r := range records {
		totalCost += r.TotalCost
	}

	if days == 0 {
		// No historical data — return a zero recommendation.
		return &SavingsPlanRecommendation{
			Timestamp:          time.Now().UTC(),
			AnalysisPeriodDays: 30,
			CurrentMonthlyCost: 0,
			EstimatedSavings:   0,
			RecommendedPlan:    "savings_plan",
			CommitmentAmount:   0,
			CoveragePercent:    0,
		}, nil
	}

	avgDailyCost := totalCost / float64(days)
	currentMonthlyCost := avgDailyCost * 30.0

	// Estimate savings from Savings Plans (typically 20-30% discount)
	// and Reserved Instances (typically 30-40% discount).
	savingsPlanDiscount := 0.25  // 25% average discount
	reservedDiscount := 0.35     // 35% average discount

	savingsPlanSavings := currentMonthlyCost * savingsPlanDiscount
	reservedSavings := currentMonthlyCost * reservedDiscount

	// Recommend the plan with higher savings.
	var recommendedPlan string
	var estimatedSavings float64
	var commitmentAmount float64
	var coveragePct float64

	if reservedSavings > savingsPlanSavings {
		recommendedPlan = "reserved_instance"
		estimatedSavings = reservedSavings
		// Commitment is the discounted monthly cost.
		commitmentAmount = currentMonthlyCost * (1 - reservedDiscount)
		coveragePct = 80.0 // typical RI coverage target
	} else {
		recommendedPlan = "savings_plan"
		estimatedSavings = savingsPlanSavings
		commitmentAmount = currentMonthlyCost * (1 - savingsPlanDiscount)
		coveragePct = 70.0 // typical SP coverage target
	}

	// Ensure savings are non-negative.
	if estimatedSavings < 0 {
		estimatedSavings = 0
	}

	rec := &SavingsPlanRecommendation{
		Timestamp:          time.Now().UTC(),
		AnalysisPeriodDays: 30,
		CurrentMonthlyCost: currentMonthlyCost,
		EstimatedSavings:   estimatedSavings,
		RecommendedPlan:    recommendedPlan,
		CommitmentAmount:   commitmentAmount,
		CoveragePercent:    coveragePct,
	}

	m.logger.Info("savings plan evaluation complete",
		"currentMonthlyCost", currentMonthlyCost,
		"estimatedSavings", estimatedSavings,
		"recommendedPlan", recommendedPlan,
	)

	return rec, nil
}
