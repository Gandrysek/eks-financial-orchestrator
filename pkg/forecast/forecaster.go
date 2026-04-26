package forecast

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/go-logr/logr"

	v1alpha1 "github.com/eks-financial-orchestrator/api/v1alpha1"
	"github.com/eks-financial-orchestrator/pkg/collector"
)

const (
	// minHistoryDays is the minimum number of daily records required for forecasting.
	minHistoryDays = 14

	// defaultSeasonLength is the weekly seasonality period.
	defaultSeasonLength = 7

	// Holt-Winters smoothing parameters.
	defaultAlpha = 0.3
	defaultBeta  = 0.1
	defaultGamma = 0.3
)

// DefaultForecaster implements the Forecaster interface using Holt-Winters
// triple exponential smoothing.
type DefaultForecaster struct {
	logger logr.Logger
}

// NewDefaultForecaster creates a new DefaultForecaster.
func NewDefaultForecaster(logger logr.Logger) *DefaultForecaster {
	return &DefaultForecaster{logger: logger}
}

// GenerateForecast produces cost forecasts for 7, 14, and 30 days using
// Holt-Winters triple exponential smoothing on the provided history.
// Requires at least 14 days of history (2 weeks for seasonality).
func (f *DefaultForecaster) GenerateForecast(ctx context.Context, history []collector.DailyCostRecord) (*Forecast, error) {
	if len(history) < minHistoryDays {
		return nil, errors.New("insufficient history: need at least 14 days of data")
	}

	// Extract daily cost values.
	data := make([]float64, len(history))
	for i, rec := range history {
		data[i] = rec.TotalCost
	}

	// Determine namespace from the first record.
	namespace := ""
	if len(history) > 0 {
		namespace = history[0].Namespace
	}

	// Generate forecasts for each horizon.
	forecastHorizons := []int{7, 14, 30}
	periods := make([]ForecastPeriod, 0, len(forecastHorizons))

	for _, days := range forecastHorizons {
		raw := holtWintersForecast(data, defaultSeasonLength, defaultAlpha, defaultBeta, defaultGamma, days)

		// Sum the daily forecasts to get total cost over the period.
		totalCost := 0.0
		for _, v := range raw {
			// Clamp individual daily forecasts to non-negative.
			if v < 0 {
				v = 0
			}
			totalCost += v
		}

		// Ensure total forecasted cost is non-negative.
		if totalCost < 0 {
			totalCost = 0
		}

		periods = append(periods, ForecastPeriod{
			Days:           days,
			ForecastedCost: totalCost,
		})
	}

	// Populate confidence intervals for each period.
	// Use forecast error standard deviation estimated from the raw daily forecasts.
	for i := range periods {
		fc := periods[i].ForecastedCost
		// Estimate uncertainty: grows with forecast horizon.
		// Use 10% of forecasted cost as base stddev, scaled by sqrt(days/7).
		stddev := fc * 0.10 * math.Sqrt(float64(periods[i].Days)/7.0)
		z := 1.96 // 95% confidence
		lower := fc - z*stddev
		upper := fc + z*stddev
		if lower < 0 {
			lower = 0
		}
		periods[i].LowerBound = lower
		periods[i].UpperBound = upper
		periods[i].Confidence = 0.95
	}

	forecast := &Forecast{
		GeneratedAt: time.Now().UTC(),
		Namespace:   namespace,
		Periods:     periods,
	}

	f.logger.V(1).Info("forecast generated",
		"namespace", namespace,
		"periods", len(periods),
		"historyDays", len(history),
	)

	return forecast, nil
}

// GetConfidenceInterval calculates lower and upper bounds for a forecast
// based on the standard deviation of forecast errors.
func (f *DefaultForecaster) GetConfidenceInterval(fc *Forecast, confidenceLevel float64) *ConfidenceInterval {
	if fc == nil || len(fc.Periods) == 0 {
		return &ConfidenceInterval{
			ConfidenceLevel: confidenceLevel,
		}
	}

	// Calculate the mean forecasted cost across periods.
	mean := 0.0
	for _, p := range fc.Periods {
		mean += p.ForecastedCost
	}
	mean /= float64(len(fc.Periods))

	// Calculate standard deviation of forecasted costs across periods.
	variance := 0.0
	for _, p := range fc.Periods {
		diff := p.ForecastedCost - mean
		variance += diff * diff
	}
	if len(fc.Periods) > 1 {
		variance /= float64(len(fc.Periods) - 1)
	}
	stddev := math.Sqrt(variance)

	// Use a z-score approximation for the confidence level.
	z := zScore(confidenceLevel)

	// Calculate bounds using the first period as the representative forecast.
	forecastedCost := fc.Periods[0].ForecastedCost

	lowerBound := forecastedCost - z*stddev
	upperBound := forecastedCost + z*stddev

	// Clamp lower bound to 0 (costs can't be negative).
	if lowerBound < 0 {
		lowerBound = 0
	}

	// Ensure lower_bound <= forecasted_cost <= upper_bound.
	if lowerBound > forecastedCost {
		lowerBound = forecastedCost
	}
	if upperBound < forecastedCost {
		upperBound = forecastedCost
	}

	return &ConfidenceInterval{
		LowerBound:      lowerBound,
		UpperBound:      upperBound,
		ConfidenceLevel: confidenceLevel,
	}
}

// DetectSeasonality identifies weekly and monthly patterns in cost data.
func (f *DefaultForecaster) DetectSeasonality(history []collector.DailyCostRecord) *SeasonalityPattern {
	pattern := &SeasonalityPattern{}

	if len(history) < 14 {
		return pattern
	}

	// --- Weekly pattern detection ---
	// Calculate average cost per day of week.
	weekdaySums := make([]float64, 7)
	weekdayCounts := make([]int, 7)
	for _, rec := range history {
		dow := int(rec.Date.Weekday())
		weekdaySums[dow] += rec.TotalCost
		weekdayCounts[dow]++
	}

	weeklyFactors := make([]float64, 7)
	overallMean := 0.0
	totalCount := 0
	for i := 0; i < 7; i++ {
		if weekdayCounts[i] > 0 {
			weeklyFactors[i] = weekdaySums[i] / float64(weekdayCounts[i])
			overallMean += weekdaySums[i]
			totalCount += weekdayCounts[i]
		}
	}
	if totalCount > 0 {
		overallMean /= float64(totalCount)
	}

	// Normalize weekly factors relative to overall mean.
	if overallMean > 0 {
		for i := 0; i < 7; i++ {
			weeklyFactors[i] /= overallMean
		}
	}

	// Detect weekly pattern: check if coefficient of variation of day-of-week
	// averages exceeds a threshold.
	weeklyCV := coefficientOfVariation(weeklyFactors)
	if weeklyCV > 0.05 {
		pattern.HasWeeklyPattern = true
		pattern.WeeklyFactors = weeklyFactors
	}

	// --- Monthly pattern detection ---
	// Calculate average cost per day of month.
	monthdaySums := make([]float64, 31)
	monthdayCounts := make([]int, 31)
	for _, rec := range history {
		dom := rec.Date.Day() - 1 // 0-indexed
		if dom >= 0 && dom < 31 {
			monthdaySums[dom] += rec.TotalCost
			monthdayCounts[dom]++
		}
	}

	monthlyFactors := make([]float64, 31)
	for i := 0; i < 31; i++ {
		if monthdayCounts[i] > 0 {
			monthlyFactors[i] = monthdaySums[i] / float64(monthdayCounts[i])
		}
	}

	// Normalize monthly factors.
	if overallMean > 0 {
		for i := 0; i < 31; i++ {
			if monthdayCounts[i] > 0 {
				monthlyFactors[i] /= overallMean
			}
		}
	}

	// Only consider days that have data for CV calculation.
	var activeMF []float64
	for i := 0; i < 31; i++ {
		if monthdayCounts[i] > 0 {
			activeMF = append(activeMF, monthlyFactors[i])
		}
	}
	monthlyCV := coefficientOfVariation(activeMF)
	if monthlyCV > 0.05 {
		pattern.HasMonthlyPattern = true
		pattern.MonthlyFactors = monthlyFactors
	}

	return pattern
}

// CheckThresholds compares forecasted costs against budget thresholds defined
// in the provided policies and returns any breaches.
func (f *DefaultForecaster) CheckThresholds(ctx context.Context, fc *Forecast, policies []*v1alpha1.FinancialPolicy) ([]ThresholdBreach, error) {
	if fc == nil {
		return nil, nil
	}

	var breaches []ThresholdBreach

	for _, policy := range policies {
		if policy == nil {
			continue
		}

		monthlyLimit := policy.Spec.Budget.MonthlyLimit
		if monthlyLimit <= 0 {
			continue
		}

		thresholds := policy.Spec.Budget.AlertThresholds
		if len(thresholds) == 0 {
			continue
		}

		for _, period := range fc.Periods {
			for _, thresholdPct := range thresholds {
				thresholdValue := monthlyLimit * thresholdPct / 100.0
				if period.ForecastedCost > thresholdValue {
					breaches = append(breaches, ThresholdBreach{
						Namespace:      fc.Namespace,
						ThresholdPct:   thresholdPct,
						BudgetLimit:    monthlyLimit,
						ForecastedCost: period.ForecastedCost,
						PeriodDays:     period.Days,
					})
				}
			}
		}
	}

	return breaches, nil
}

// zScore returns an approximate z-score for common confidence levels.
func zScore(confidenceLevel float64) float64 {
	switch {
	case confidenceLevel >= 0.99:
		return 2.576
	case confidenceLevel >= 0.95:
		return 1.960
	case confidenceLevel >= 0.90:
		return 1.645
	case confidenceLevel >= 0.80:
		return 1.282
	default:
		return 1.0
	}
}

// coefficientOfVariation calculates the CV of a slice of float64 values.
func coefficientOfVariation(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}

	mean := 0.0
	for _, v := range values {
		mean += v
	}
	mean /= float64(len(values))

	if mean == 0 {
		return 0
	}

	variance := 0.0
	for _, v := range values {
		diff := v - mean
		variance += diff * diff
	}
	variance /= float64(len(values))

	return math.Sqrt(variance) / math.Abs(mean)
}
