package forecast

import (
	"context"
	"time"

	v1alpha1 "github.com/eks-financial-orchestrator/api/v1alpha1"
	"github.com/eks-financial-orchestrator/pkg/collector"
)

// Forecast represents a cost forecast with confidence intervals.
type Forecast struct {
	GeneratedAt time.Time        `json:"generated_at"`
	Namespace   string           `json:"namespace"`
	Periods     []ForecastPeriod `json:"periods"`
}

// ForecastPeriod holds the forecast for a specific time horizon.
type ForecastPeriod struct {
	Days           int     `json:"days"`
	ForecastedCost float64 `json:"forecasted_cost"`
	LowerBound     float64 `json:"lower_bound"`
	UpperBound     float64 `json:"upper_bound"`
	Confidence     float64 `json:"confidence"`
}

// ConfidenceInterval holds the lower and upper bounds for a forecast.
type ConfidenceInterval struct {
	LowerBound      float64 `json:"lower_bound"`
	UpperBound      float64 `json:"upper_bound"`
	ConfidenceLevel float64 `json:"confidence_level"`
}

// SeasonalityPattern describes detected weekly and monthly patterns in cost data.
type SeasonalityPattern struct {
	HasWeeklyPattern  bool      `json:"has_weekly_pattern"`
	HasMonthlyPattern bool      `json:"has_monthly_pattern"`
	WeeklyFactors     []float64 `json:"weekly_factors,omitempty"`  // 7 values, one per day of week
	MonthlyFactors    []float64 `json:"monthly_factors,omitempty"` // up to 31 values, one per day of month
}

// ThresholdBreach represents a forecasted cost exceeding a budget threshold.
type ThresholdBreach struct {
	Namespace      string  `json:"namespace"`
	ThresholdPct   float64 `json:"threshold_pct"`
	BudgetLimit    float64 `json:"budget_limit"`
	ForecastedCost float64 `json:"forecasted_cost"`
	PeriodDays     int     `json:"period_days"`
}

// Forecaster defines the interface for cost forecasting.
type Forecaster interface {
	// GenerateForecast produces cost forecasts for 7, 14, and 30 days
	// using historical data from the last 90 days.
	GenerateForecast(ctx context.Context, history []collector.DailyCostRecord) (*Forecast, error)

	// GetConfidenceInterval calculates lower and upper bounds for a forecast.
	GetConfidenceInterval(forecast *Forecast, confidenceLevel float64) *ConfidenceInterval

	// DetectSeasonality identifies weekly and monthly patterns in cost data.
	DetectSeasonality(history []collector.DailyCostRecord) *SeasonalityPattern

	// CheckThresholds compares forecasted costs against budget thresholds
	// and returns any breaches.
	CheckThresholds(ctx context.Context, forecast *Forecast, policies []*v1alpha1.FinancialPolicy) ([]ThresholdBreach, error)
}
