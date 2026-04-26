package generators

import (
	"time"

	"github.com/eks-financial-orchestrator/pkg/collector"
	"github.com/eks-financial-orchestrator/pkg/forecast"
	"pgregory.net/rapid"
)

// Forecast generates a Forecast with exactly 3 periods (7, 14, 30 days).
func Forecast(t *rapid.T) *forecast.Forecast {
	return &forecast.Forecast{
		GeneratedAt: time.Now().UTC(),
		Namespace:   rapid.StringMatching(`[a-z]{3,15}`).Draw(t, "namespace"),
		Periods: []forecast.ForecastPeriod{
			ForecastPeriod(t, 7),
			ForecastPeriod(t, 14),
			ForecastPeriod(t, 30),
		},
	}
}

// ForecastPeriod generates a single ForecastPeriod for the given number of days.
// It ensures the confidence interval invariant: lower_bound <= forecasted_cost <= upper_bound.
func ForecastPeriod(t *rapid.T, days int) forecast.ForecastPeriod {
	forecastedCost := rapid.Float64Range(0, 10000).Draw(t, "forecasted_cost")
	// Generate bounds that respect the invariant.
	lowerBound := rapid.Float64Range(0, forecastedCost).Draw(t, "lower_bound")
	upperBound := rapid.Float64Range(forecastedCost, forecastedCost+5000).Draw(t, "upper_bound")
	confidence := rapid.Float64Range(0.5, 0.99).Draw(t, "confidence")

	return forecast.ForecastPeriod{
		Days:           days,
		ForecastedCost: forecastedCost,
		LowerBound:     lowerBound,
		UpperBound:     upperBound,
		Confidence:     confidence,
	}
}

// DailyCostHistory90Days generates 90 days of sequential cost history suitable for forecasting.
func DailyCostHistory90Days(t *rapid.T) []collector.DailyCostRecord {
	return DailyCostHistory(t, 90)
}
