package collector

import (
	"context"
	"fmt"
	"time"

	"github.com/eks-financial-orchestrator/pkg/db"
	"github.com/jackc/pgx/v5"
)

// CostStore defines the interface for persisting and querying cost records.
type CostStore interface {
	// StoreCostRecords persists aggregated cost data to the cost_records table.
	StoreCostRecords(ctx context.Context, costs *AggregatedCosts) error

	// QueryCostRecords retrieves cost records for the given time range,
	// optionally filtered by namespace. Pass an empty namespace to retrieve
	// all records.
	QueryCostRecords(ctx context.Context, start, end time.Time, namespace string) ([]DailyCostRecord, error)
}

// TimescaleDBCostStore implements CostStore using a TimescaleDB/PostgreSQL
// database via the pkg/db connection pool.
type TimescaleDBCostStore struct {
	database *db.DB
}

// NewTimescaleDBCostStore creates a new TimescaleDBCostStore backed by the
// given database connection.
func NewTimescaleDBCostStore(database *db.DB) *TimescaleDBCostStore {
	return &TimescaleDBCostStore{database: database}
}

// StoreCostRecords inserts aggregated cost data into the cost_records table
// using a batch insert for efficiency. For each namespace in the aggregated
// costs, it inserts one row per pod. If a namespace has no pod-level data,
// a single summary row is inserted for the namespace.
func (s *TimescaleDBCostStore) StoreCostRecords(ctx context.Context, costs *AggregatedCosts) error {
	if costs == nil {
		return fmt.Errorf("costs must not be nil")
	}

	const insertSQL = `INSERT INTO cost_records (
		time, namespace, service, team, pod_name, node_name,
		instance_type, purchase_option,
		cpu_cost, memory_cost, network_cost, storage_cost,
		total_cost, is_approximate
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
	ON CONFLICT DO NOTHING`

	batch := &pgx.Batch{}

	for nsName, nsCost := range costs.ByNamespace {
		if len(nsCost.ByPod) > 0 {
			// Insert one row per pod.
			for podName, podCost := range nsCost.ByPod {
				batch.Queue(insertSQL,
					costs.Timestamp,       // time
					nsName,                // namespace
					"",                    // service (pod-level rows don't carry a single service)
					nsCost.Team,           // team
					podName,               // pod_name
					"",                    // node_name (not tracked at aggregation level)
					"",                    // instance_type
					"",                    // purchase_option
					0.0,                   // cpu_cost (individual breakdown not available)
					0.0,                   // memory_cost
					0.0,                   // network_cost
					0.0,                   // storage_cost
					podCost,               // total_cost
					nsCost.IsApproximate,  // is_approximate
				)
			}
		} else {
			// No pod-level data — insert a summary row for the namespace.
			batch.Queue(insertSQL,
				costs.Timestamp,       // time
				nsName,                // namespace
				"",                    // service
				nsCost.Team,           // team
				"",                    // pod_name
				"",                    // node_name
				"",                    // instance_type
				"",                    // purchase_option
				0.0,                   // cpu_cost
				0.0,                   // memory_cost
				0.0,                   // network_cost
				0.0,                   // storage_cost
				nsCost.TotalCost,      // total_cost
				nsCost.IsApproximate,  // is_approximate
			)
		}
	}

	if batch.Len() == 0 {
		return nil
	}

	br := s.database.Pool.SendBatch(ctx, batch)
	defer br.Close()

	for i := 0; i < batch.Len(); i++ {
		if _, err := br.Exec(); err != nil {
			return fmt.Errorf("executing batch insert row %d: %w", i, err)
		}
	}

	return nil
}

// QueryCostRecords retrieves daily cost records from the cost_records table
// for the given time range. If namespace is non-empty, results are filtered
// to that namespace. Results are aggregated by day and namespace using
// TimescaleDB's time_bucket function.
func (s *TimescaleDBCostStore) QueryCostRecords(ctx context.Context, start, end time.Time, namespace string) ([]DailyCostRecord, error) {
	query := `SELECT
		time_bucket('1 day', time) AS day,
		namespace,
		SUM(total_cost) AS total_cost
	FROM cost_records
	WHERE time >= $1 AND time < $2`

	args := []interface{}{start, end}

	if namespace != "" {
		query += ` AND namespace = $3`
		args = append(args, namespace)
	}

	query += ` GROUP BY day, namespace ORDER BY day ASC`

	rows, err := s.database.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying cost records: %w", err)
	}
	defer rows.Close()

	var records []DailyCostRecord
	for rows.Next() {
		var r DailyCostRecord
		if err := rows.Scan(&r.Date, &r.Namespace, &r.TotalCost); err != nil {
			return nil, fmt.Errorf("scanning cost record row: %w", err)
		}
		r.DayOfWeek = int(r.Date.Weekday())
		r.DayOfMonth = r.Date.Day()
		records = append(records, r)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating cost record rows: %w", err)
	}

	return records, nil
}
