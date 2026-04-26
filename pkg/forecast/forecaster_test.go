package forecast

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/go-logr/logr"

	v1alpha1 "github.com/eks-financial-orchestrator/api/v1alpha1"
	"github.com/eks-financial-orchestrator/pkg/collector"
)

// helper: generate N days of flat cost history.
func flatHistory(days int, costPerDay float64) []collector.DailyCostRecord {
	records := make([]collector.DailyCostRecord, days)
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < days; i++ {
		d := start.AddDate(0, 0, i)
		records[i] = collector.DailyCostRecord{
			Date:       d,
			Namespace:  "default",
			TotalCost:  costPerDay,
			DayOfWeek:  int(d.Weekday()),
			DayOfMonth: d.Day(),
		}
	}
	return records
}

// helper: generate N days of history with weekly seasonality.
func weeklySeasonalHistory(days int) []collector.DailyCostRecord {
	records := make([]collector.DailyCostRecord, days)
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC) // Monday
	// Weekday multipliers: Mon=1.0, Tue=1.1, Wed=1.2, Thu=1.1, Fri=1.3, Sat=0.5, Sun=0.4
	multipliers := []float64{0.4, 1.0, 1.1, 1.2, 1.1, 1.3, 0.5} // Sunday=0, Monday=1, ...
	baseCost := 100.0
	for i := 0; i < days; i++ {
		d := start.AddDate(0, 0, i)
		dow := int(d.Weekday())
		records[i] = collector.DailyCostRecord{
			Date:       d,
			Namespace:  "production",
			TotalCost:  baseCost * multipliers[dow],
			DayOfWeek:  dow,
			DayOfMonth: d.Day(),
		}
	}
	return records
}

func TestGenerateForecast_90Days_Produces3Periods(t *testing.T) {
	f := NewDefaultForecaster(logr.Discard())
	history := flatHistory(90, 100.0)

	forecast, err := f.GenerateForecast(context.Background(), history)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(forecast.Periods) != 3 {
		t.Fatalf("expected 3 periods, got %d", len(forecast.Periods))
	}

	expectedDays := []int{7, 14, 30}
	for i, p := range forecast.Periods {
		if p.Days != expectedDays[i] {
			t.Errorf("period %d: expected %d days, got %d", i, expectedDays[i], p.Days)
		}
	}
}

func TestGenerateForecast_FlatHistory(t *testing.T) {
	f := NewDefaultForecaster(logr.Discard())
	history := flatHistory(90, 50.0)

	forecast, err := f.GenerateForecast(context.Background(), history)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(forecast.Periods) != 3 {
		t.Fatalf("expected 3 periods, got %d", len(forecast.Periods))
	}

	// For flat history, the 7-day forecast should be close to 7 * 50 = 350.
	sevenDay := forecast.Periods[0].ForecastedCost
	if math.Abs(sevenDay-350.0) > 50.0 {
		t.Errorf("7-day forecast for flat $50/day history: expected ~350, got %.2f", sevenDay)
	}

	// All forecasted costs should be non-negative.
	for _, p := range forecast.Periods {
		if p.ForecastedCost < 0 {
			t.Errorf("period %d days: forecasted cost is negative: %.2f", p.Days, p.ForecastedCost)
		}
	}
}

func TestGenerateForecast_InsufficientData(t *testing.T) {
	f := NewDefaultForecaster(logr.Discard())

	// 13 days is below the 14-day minimum.
	history := flatHistory(13, 100.0)

	_, err := f.GenerateForecast(context.Background(), history)
	if err == nil {
		t.Fatal("expected error for insufficient data, got nil")
	}
}

func TestGenerateForecast_AllNonNegative(t *testing.T) {
	f := NewDefaultForecaster(logr.Discard())

	// Use a history with some zero-cost days.
	history := flatHistory(90, 0.0)

	forecast, err := f.GenerateForecast(context.Background(), history)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, p := range forecast.Periods {
		if p.ForecastedCost < 0 {
			t.Errorf("period %d days: forecasted cost is negative: %.2f", p.Days, p.ForecastedCost)
		}
	}
}

func TestGetConfidenceInterval_BoundsOrder(t *testing.T) {
	f := NewDefaultForecaster(logr.Discard())

	fc := &Forecast{
		GeneratedAt: time.Now(),
		Namespace:   "test",
		Periods: []ForecastPeriod{
			{Days: 7, ForecastedCost: 700},
			{Days: 14, ForecastedCost: 1500},
			{Days: 30, ForecastedCost: 3200},
		},
	}

	ci := f.GetConfidenceInterval(fc, 0.95)

	if ci.LowerBound > fc.Periods[0].ForecastedCost {
		t.Errorf("lower bound (%.2f) > forecasted cost (%.2f)", ci.LowerBound, fc.Periods[0].ForecastedCost)
	}
	if ci.UpperBound < fc.Periods[0].ForecastedCost {
		t.Errorf("upper bound (%.2f) < forecasted cost (%.2f)", ci.UpperBound, fc.Periods[0].ForecastedCost)
	}
	if ci.LowerBound < 0 {
		t.Errorf("lower bound is negative: %.2f", ci.LowerBound)
	}
}

func TestGetConfidenceInterval_NilForecast(t *testing.T) {
	f := NewDefaultForecaster(logr.Discard())

	ci := f.GetConfidenceInterval(nil, 0.95)
	if ci == nil {
		t.Fatal("expected non-nil confidence interval for nil forecast")
	}
}

func TestCheckThresholds_DetectsBreaches(t *testing.T) {
	f := NewDefaultForecaster(logr.Discard())

	fc := &Forecast{
		Namespace: "production",
		Periods: []ForecastPeriod{
			{Days: 7, ForecastedCost: 900},
			{Days: 14, ForecastedCost: 1800},
			{Days: 30, ForecastedCost: 3500},
		},
	}

	policies := []*v1alpha1.FinancialPolicy{
		{
			Spec: v1alpha1.FinancialPolicySpec{
				TargetNamespace: "production",
				Budget: v1alpha1.BudgetSpec{
					MonthlyLimit:    1000,
					AlertThresholds: []float64{50, 80, 100},
				},
			},
		},
	}

	breaches, err := f.CheckThresholds(context.Background(), fc, policies)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// With monthly limit 1000 and thresholds at 50% (500), 80% (800), 100% (1000):
	// 7-day forecast 900 > 500 (breach), > 800 (breach), < 1000 (no breach) => 2 breaches
	// 14-day forecast 1800 > all thresholds => 3 breaches
	// 30-day forecast 3500 > all thresholds => 3 breaches
	// Total: 2 + 3 + 3 = 8 breaches
	if len(breaches) != 8 {
		t.Errorf("expected 8 breaches, got %d", len(breaches))
		for _, b := range breaches {
			t.Logf("  breach: period=%d, threshold=%.0f%%, forecasted=%.2f, limit=%.2f",
				b.PeriodDays, b.ThresholdPct, b.ForecastedCost, b.BudgetLimit)
		}
	}
}

func TestCheckThresholds_NoBreaches(t *testing.T) {
	f := NewDefaultForecaster(logr.Discard())

	fc := &Forecast{
		Namespace: "staging",
		Periods: []ForecastPeriod{
			{Days: 7, ForecastedCost: 100},
			{Days: 14, ForecastedCost: 200},
			{Days: 30, ForecastedCost: 400},
		},
	}

	policies := []*v1alpha1.FinancialPolicy{
		{
			Spec: v1alpha1.FinancialPolicySpec{
				TargetNamespace: "staging",
				Budget: v1alpha1.BudgetSpec{
					MonthlyLimit:    10000,
					AlertThresholds: []float64{50, 80, 100},
				},
			},
		},
	}

	breaches, err := f.CheckThresholds(context.Background(), fc, policies)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(breaches) != 0 {
		t.Errorf("expected 0 breaches, got %d", len(breaches))
	}
}

func TestDetectSeasonality_WeeklyPattern(t *testing.T) {
	f := NewDefaultForecaster(logr.Discard())

	// 90 days of data with clear weekly seasonality.
	history := weeklySeasonalHistory(90)

	pattern := f.DetectSeasonality(history)

	if !pattern.HasWeeklyPattern {
		t.Error("expected weekly pattern to be detected")
	}

	if pattern.WeeklyFactors == nil || len(pattern.WeeklyFactors) != 7 {
		t.Errorf("expected 7 weekly factors, got %v", pattern.WeeklyFactors)
	}
}

func TestDetectSeasonality_InsufficientData(t *testing.T) {
	f := NewDefaultForecaster(logr.Discard())

	history := flatHistory(7, 100.0)
	pattern := f.DetectSeasonality(history)

	if pattern.HasWeeklyPattern {
		t.Error("should not detect weekly pattern with only 7 days of data")
	}
	if pattern.HasMonthlyPattern {
		t.Error("should not detect monthly pattern with only 7 days of data")
	}
}

// --- Scheduler tests ---

type mockCostReader struct {
	records []collector.DailyCostRecord
	err     error
}

func (m *mockCostReader) ReadDailyCosts(_ context.Context, _ int) ([]collector.DailyCostRecord, error) {
	return m.records, m.err
}

type mockForecastStore struct {
	stored   *Forecast
	storeErr error
}

func (m *mockForecastStore) StoreForecast(_ context.Context, fc *Forecast) error {
	if m.storeErr != nil {
		return m.storeErr
	}
	m.stored = fc
	return nil
}

func TestScheduler_RunOnce_Success(t *testing.T) {
	reader := &mockCostReader{records: flatHistory(90, 100.0)}
	store := &mockForecastStore{}
	forecaster := NewDefaultForecaster(logr.Discard())
	scheduler := NewForecastScheduler(forecaster, reader, store, logr.Discard())

	scheduler.RunOnce(context.Background())

	if store.stored == nil {
		t.Fatal("expected forecast to be stored")
	}
	if len(store.stored.Periods) != 3 {
		t.Errorf("expected 3 periods, got %d", len(store.stored.Periods))
	}
}

func TestScheduler_RunOnce_ReadFailure_NoOverwrite(t *testing.T) {
	reader := &mockCostReader{err: errors.New("db connection failed")}
	store := &mockForecastStore{}
	forecaster := NewDefaultForecaster(logr.Discard())
	scheduler := NewForecastScheduler(forecaster, reader, store, logr.Discard())

	scheduler.RunOnce(context.Background())

	if store.stored != nil {
		t.Error("forecast should not be stored when read fails")
	}
}

func TestScheduler_RunOnce_StoreFailure_NoOverwrite(t *testing.T) {
	reader := &mockCostReader{records: flatHistory(90, 100.0)}
	store := &mockForecastStore{storeErr: errors.New("write failed")}
	forecaster := NewDefaultForecaster(logr.Discard())
	scheduler := NewForecastScheduler(forecaster, reader, store, logr.Discard())

	scheduler.RunOnce(context.Background())

	// Store should not have a forecast since the write failed.
	if store.stored != nil {
		t.Error("forecast should not be stored when store fails")
	}
}
