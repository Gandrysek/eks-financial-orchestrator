package generators

import (
	"fmt"
	"time"

	"github.com/eks-financial-orchestrator/pkg/instance"
	"pgregory.net/rapid"
)

// InstanceMixAnalysis generates an InstanceMixAnalysis with realistic values.
func InstanceMixAnalysis(t *rapid.T) *instance.InstanceMixAnalysis {
	totalNodes := rapid.IntRange(1, 50).Draw(t, "total_nodes")
	currentCost := rapid.Float64Range(100, 50000).Draw(t, "current_cost")
	optimalCost := rapid.Float64Range(0, currentCost).Draw(t, "optimal_cost")

	purchaseOptions := []string{"on_demand", "spot", "reserved", "savings_plan"}
	numOptions := rapid.IntRange(1, len(purchaseOptions)).Draw(t, "num_options")

	byPurchaseOption := make(map[string]*instance.PurchaseOptionStats, numOptions)
	remainingNodes := totalNodes
	remainingPct := 100.0

	for i := 0; i < numOptions; i++ {
		option := purchaseOptions[i]
		var count int
		var pct float64

		if i == numOptions-1 {
			// Last option gets the remainder.
			count = remainingNodes
			pct = remainingPct
		} else {
			count = rapid.IntRange(0, remainingNodes).Draw(t, "option_count")
			pct = rapid.Float64Range(0, remainingPct).Draw(t, "option_pct")
			remainingNodes -= count
			remainingPct -= pct
		}

		byPurchaseOption[option] = &instance.PurchaseOptionStats{
			Count:      count,
			Percentage: pct,
			Cost:       rapid.Float64Range(0, 10000).Draw(t, "option_cost"),
		}
	}

	return &instance.InstanceMixAnalysis{
		Timestamp:        time.Now().UTC(),
		TotalNodes:       totalNodes,
		ByPurchaseOption: byPurchaseOption,
		CurrentCost:      currentCost,
		OptimalCost:      optimalCost,
		PotentialSavings: currentCost - optimalCost,
	}
}

// PurchaseOptionStats generates a PurchaseOptionStats with realistic values.
func PurchaseOptionStats(t *rapid.T) *instance.PurchaseOptionStats {
	return &instance.PurchaseOptionStats{
		Count:      rapid.IntRange(0, 50).Draw(t, "count"),
		Percentage: rapid.Float64Range(0, 100).Draw(t, "percentage"),
		Cost:       rapid.Float64Range(0, 10000).Draw(t, "cost"),
	}
}

// OptimizationRecommendation generates an OptimizationRecommendation with realistic values.
func OptimizationRecommendation(t *rapid.T) *instance.OptimizationRecommendation {
	purchaseOptions := []string{"on_demand", "spot", "reserved", "savings_plan"}

	currentMix := make(map[string]float64, len(purchaseOptions))
	recommendedMix := make(map[string]float64, len(purchaseOptions))
	for _, opt := range purchaseOptions {
		currentMix[opt] = rapid.Float64Range(0, 100).Draw(t, "current_"+opt)
		recommendedMix[opt] = rapid.Float64Range(0, 100).Draw(t, "recommended_"+opt)
	}

	numAffected := rapid.IntRange(0, 10).Draw(t, "num_affected_nodes")
	affectedNodes := make([]string, numAffected)
	for i := 0; i < numAffected; i++ {
		affectedNodes[i] = fmt.Sprintf("node-%d", rapid.IntRange(1, 100).Draw(t, "node_id"))
	}

	reasons := []string{
		"High Spot availability detected",
		"On-Demand costs exceed threshold",
		"Reserved Instance coverage gap",
		"Savings Plan opportunity identified",
	}

	return &instance.OptimizationRecommendation{
		ID:              fmt.Sprintf("rec-%d", rapid.IntRange(1, 10000).Draw(t, "rec_id")),
		Timestamp:       time.Now().UTC(),
		CurrentMix:      currentMix,
		RecommendedMix:  recommendedMix,
		ExpectedSavings: rapid.Float64Range(0, 5000).Draw(t, "expected_savings"),
		Reason:          reasons[rapid.IntRange(0, len(reasons)-1).Draw(t, "reason_idx")],
		AffectedNodes:   affectedNodes,
		PolicyCompliant: rapid.Bool().Draw(t, "policy_compliant"),
	}
}
