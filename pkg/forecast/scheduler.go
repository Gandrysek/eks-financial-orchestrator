package forecast

import (
	"context"
	"time"

	"github.com/go-logr/logr"

	"github.com/eks-financial-orchestrator/pkg/collector"
)

// CostDataReader provides access to historical cost data for forecasting.
type CostDataReader interface {
	// ReadDailyCosts returns daily cost records for the given number of past days.
	ReadDailyCosts(ctx context.Context, days int) ([]collector.DailyCostRecord, error)
}

// ForecastStore persists generated forecasts.
type ForecastStore interface {
	// StoreForecast saves a forecast. Returns an error if the write fails.
	StoreForecast(ctx context.Context, forecast *Forecast) error
}

// ForecastScheduler runs periodic forecast recalculations.
type ForecastScheduler struct {
	forecaster Forecaster
	reader     CostDataReader
	store      ForecastStore
	logger     logr.Logger
	interval   time.Duration
}

// NewForecastScheduler creates a new ForecastScheduler that recalculates
// forecasts at the given interval (default: every 6 hours).
func NewForecastScheduler(forecaster Forecaster, reader CostDataReader, store ForecastStore, logger logr.Logger) *ForecastScheduler {
	return &ForecastScheduler{
		forecaster: forecaster,
		reader:     reader,
		store:      store,
		logger:     logger,
		interval:   6 * time.Hour,
	}
}

// Start begins the periodic forecast recalculation loop. It runs every 6 hours
// until the context is cancelled. It also runs once immediately on start.
func (s *ForecastScheduler) Start(ctx context.Context) {
	// Run once immediately.
	s.RunOnce(ctx)

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("forecast scheduler stopped")
			return
		case <-ticker.C:
			s.RunOnce(ctx)
		}
	}
}

// RunOnce reads 90 days of history, generates a forecast, and stores the result.
// On recalculation failure, the last valid forecast is NOT overwritten.
func (s *ForecastScheduler) RunOnce(ctx context.Context) {
	s.logger.V(1).Info("starting forecast recalculation")

	history, err := s.reader.ReadDailyCosts(ctx, 90)
	if err != nil {
		s.logger.Error(err, "failed to read cost history for forecast recalculation")
		return
	}

	forecast, err := s.forecaster.GenerateForecast(ctx, history)
	if err != nil {
		s.logger.Error(err, "failed to generate forecast during recalculation")
		return
	}

	if err := s.store.StoreForecast(ctx, forecast); err != nil {
		s.logger.Error(err, "failed to store forecast during recalculation")
		return
	}

	s.logger.V(1).Info("forecast recalculation completed successfully",
		"namespace", forecast.Namespace,
		"periods", len(forecast.Periods),
	)
}
