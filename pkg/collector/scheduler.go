package collector

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
)

// DefaultCollectionInterval is the default interval between cost collection
// cycles (5 minutes).
const DefaultCollectionInterval = 5 * time.Minute

// CostCollectionScheduler runs the full cost collection pipeline on a
// recurring schedule and persists results to a CostStore.
type CostCollectionScheduler struct {
	collector CostCollector
	store     CostStore
	interval  time.Duration
	logger    logr.Logger
}

// NewCostCollectionScheduler creates a new scheduler that triggers the
// cost collection pipeline at the default interval (every 5 minutes)
// and persists results via the provided store.
func NewCostCollectionScheduler(collector CostCollector, store CostStore, logger logr.Logger) *CostCollectionScheduler {
	return &CostCollectionScheduler{
		collector: collector,
		store:     store,
		interval:  DefaultCollectionInterval,
		logger:    logger.WithName("cost-scheduler"),
	}
}

// Start begins the periodic cost collection loop. It runs an initial
// collection immediately, then repeats every 5 minutes. The loop stops
// when the context is cancelled. Start blocks until the context is done.
func (s *CostCollectionScheduler) Start(ctx context.Context) error {
	s.logger.Info("starting cost collection scheduler", "interval", s.interval)

	// Run an initial collection immediately.
	if err := s.RunOnce(ctx); err != nil {
		s.logger.Error(err, "initial cost collection failed")
		// Don't return — continue with the ticker schedule.
	}

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("cost collection scheduler stopped")
			return ctx.Err()
		case <-ticker.C:
			if err := s.RunOnce(ctx); err != nil {
				s.logger.Error(err, "cost collection cycle failed")
				// Log and continue — don't crash the scheduler.
			}
		}
	}
}

// RunOnce executes a single cost collection cycle:
//  1. CollectClusterMetrics — gather K8s resource usage
//  2. FetchAWSCosts — retrieve AWS cost data for the last 24 hours
//  3. CorrelateAndAggregate — combine metrics with costs
//  4. AllocateSharedCosts — distribute shared costs across namespaces
//  5. StoreCostRecords — persist to TimescaleDB
//
// Each step is logged. Errors are returned but the scheduler's Start loop
// will log them and continue to the next cycle.
func (s *CostCollectionScheduler) RunOnce(ctx context.Context) error {
	start := time.Now()
	s.logger.Info("starting cost collection cycle")

	// Step 1: Collect cluster metrics from Kubernetes.
	s.logger.V(1).Info("step 1: collecting cluster metrics")
	metrics, err := s.collector.CollectClusterMetrics(ctx)
	if err != nil {
		return fmt.Errorf("collecting cluster metrics: %w", err)
	}
	s.logger.V(1).Info("cluster metrics collected",
		"pods", len(metrics.Pods),
		"nodes", len(metrics.Nodes),
	)

	// Step 2: Fetch AWS costs for the last 24 hours.
	s.logger.V(1).Info("step 2: fetching AWS costs")
	now := time.Now().UTC()
	awsStart := now.Add(-24 * time.Hour)
	costs, err := s.collector.FetchAWSCosts(ctx, awsStart, now)
	if err != nil {
		return fmt.Errorf("fetching AWS costs: %w", err)
	}
	s.logger.V(1).Info("AWS costs fetched",
		"totalCost", costs.TotalCost,
		"isApproximate", costs.IsApproximate,
	)

	// Step 3: Correlate K8s metrics with AWS costs and aggregate.
	s.logger.V(1).Info("step 3: correlating and aggregating")
	aggregated, err := s.collector.CorrelateAndAggregate(ctx, metrics, costs)
	if err != nil {
		return fmt.Errorf("correlating and aggregating: %w", err)
	}
	s.logger.V(1).Info("correlation complete",
		"namespaces", len(aggregated.ByNamespace),
		"totalCost", aggregated.TotalCost,
	)

	// Step 4: Allocate shared costs across namespaces.
	s.logger.V(1).Info("step 4: allocating shared costs")
	aggregated, err = s.collector.AllocateSharedCosts(ctx, aggregated)
	if err != nil {
		return fmt.Errorf("allocating shared costs: %w", err)
	}
	s.logger.V(1).Info("shared costs allocated",
		"namespaces", len(aggregated.ByNamespace),
		"totalCost", aggregated.TotalCost,
	)

	// Step 5: Persist to TimescaleDB.
	s.logger.V(1).Info("step 5: storing cost records")
	if err := s.store.StoreCostRecords(ctx, aggregated); err != nil {
		return fmt.Errorf("storing cost records: %w", err)
	}

	elapsed := time.Since(start)
	s.logger.Info("cost collection cycle complete",
		"duration", elapsed,
		"namespaces", len(aggregated.ByNamespace),
		"totalCost", aggregated.TotalCost,
	)

	return nil
}
