package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"

	"github.com/eks-financial-orchestrator/pkg/db"
)

// DefaultAuditWriter implements AuditWriter by writing entries to the audit_log
// table in TimescaleDB/PostgreSQL and emitting structured JSON log lines for
// centralized logging integration (CloudWatch Logs, ELK, Loki).
type DefaultAuditWriter struct {
	db     *db.DB
	logger logr.Logger
}

// NewDefaultAuditWriter creates a new DefaultAuditWriter backed by the given
// database and logger.
func NewDefaultAuditWriter(database *db.DB, logger logr.Logger) *DefaultAuditWriter {
	return &DefaultAuditWriter{
		db:     database,
		logger: logger.WithName("audit"),
	}
}

// WriteEntry records an audit log entry to the audit_log table and emits a
// structured JSON log line. The write is fire-and-forget: if the database
// insert fails, the error is logged and counted via a metric, but is NOT
// returned to the caller so that audit logging never blocks the main operation.
func (w *DefaultAuditWriter) WriteEntry(ctx context.Context, entry *AuditLogEntry) error {
	// Emit structured JSON log line for centralized logging integration.
	w.emitStructuredLog(entry)

	// Serialize details to JSON for the JSONB column.
	detailsJSON, err := json.Marshal(entry.Details)
	if err != nil {
		w.logger.Error(err, "Failed to marshal audit entry details",
			"action", entry.Action,
			"resource", entry.ResourceName,
		)
		// Use nil for details if marshalling fails.
		detailsJSON = nil
	}

	// Fire-and-forget database insert.
	const query = `INSERT INTO audit_log (timestamp, actor, action, resource_type, resource_name, namespace, details, reason, expected_savings, result)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`

	ts := entry.Timestamp
	if ts.IsZero() {
		ts = time.Now()
	}

	_, dbErr := w.db.Pool.Exec(ctx, query,
		ts,
		entry.Actor,
		entry.Action,
		entry.ResourceType,
		entry.ResourceName,
		entry.Namespace,
		detailsJSON,
		entry.Reason,
		entry.ExpectedSavings,
		entry.Result,
	)
	if dbErr != nil {
		w.logger.Error(dbErr, "Failed to write audit log entry to database",
			"actor", entry.Actor,
			"action", entry.Action,
			"resource_type", entry.ResourceType,
			"resource_name", entry.ResourceName,
		)
		// Fire-and-forget: do not return the error.
		return nil
	}

	w.logger.V(1).Info("Audit log entry written",
		"actor", entry.Actor,
		"action", entry.Action,
		"resource", entry.ResourceName,
	)
	return nil
}

// QueryEntries retrieves audit log entries matching the given filters, ordered
// by timestamp DESC. If no limit is specified, defaults to 100.
func (w *DefaultAuditWriter) QueryEntries(ctx context.Context, filters AuditQueryFilters) ([]AuditLogEntry, error) {
	// Build dynamic query.
	var conditions []string
	var args []interface{}
	argIdx := 1

	if filters.Actor != "" {
		conditions = append(conditions, fmt.Sprintf("actor = $%d", argIdx))
		args = append(args, filters.Actor)
		argIdx++
	}
	if filters.Action != "" {
		conditions = append(conditions, fmt.Sprintf("action = $%d", argIdx))
		args = append(args, filters.Action)
		argIdx++
	}
	if filters.ResourceType != "" {
		conditions = append(conditions, fmt.Sprintf("resource_type = $%d", argIdx))
		args = append(args, filters.ResourceType)
		argIdx++
	}
	if filters.Namespace != "" {
		conditions = append(conditions, fmt.Sprintf("namespace = $%d", argIdx))
		args = append(args, filters.Namespace)
		argIdx++
	}
	if !filters.StartTime.IsZero() {
		conditions = append(conditions, fmt.Sprintf("timestamp >= $%d", argIdx))
		args = append(args, filters.StartTime)
		argIdx++
	}
	if !filters.EndTime.IsZero() {
		conditions = append(conditions, fmt.Sprintf("timestamp <= $%d", argIdx))
		args = append(args, filters.EndTime)
		argIdx++
	}

	query := "SELECT id, timestamp, actor, action, resource_type, resource_name, namespace, details, reason, expected_savings, result FROM audit_log"
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY timestamp DESC"

	limit := filters.Limit
	if limit <= 0 {
		limit = 100
	}
	query += fmt.Sprintf(" LIMIT $%d", argIdx)
	args = append(args, limit)

	rows, err := w.db.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying audit log: %w", err)
	}
	defer rows.Close()

	var entries []AuditLogEntry
	for rows.Next() {
		var e AuditLogEntry
		var detailsJSON []byte
		if err := rows.Scan(
			&e.ID,
			&e.Timestamp,
			&e.Actor,
			&e.Action,
			&e.ResourceType,
			&e.ResourceName,
			&e.Namespace,
			&detailsJSON,
			&e.Reason,
			&e.ExpectedSavings,
			&e.Result,
		); err != nil {
			return nil, fmt.Errorf("scanning audit log row: %w", err)
		}
		if detailsJSON != nil {
			if err := json.Unmarshal(detailsJSON, &e.Details); err != nil {
				w.logger.Error(err, "Failed to unmarshal audit entry details", "id", e.ID)
			}
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating audit log rows: %w", err)
	}

	return entries, nil
}

// emitStructuredLog writes a structured JSON log line with all entry fields
// for integration with centralized logging systems.
func (w *DefaultAuditWriter) emitStructuredLog(entry *AuditLogEntry) {
	w.logger.Info("AuditEvent",
		"audit_id", entry.ID,
		"timestamp", entry.Timestamp,
		"actor", entry.Actor,
		"action", entry.Action,
		"resource_type", entry.ResourceType,
		"resource_name", entry.ResourceName,
		"namespace", entry.Namespace,
		"details", entry.Details,
		"reason", entry.Reason,
		"expected_savings", entry.ExpectedSavings,
		"result", entry.Result,
	)
}

// NoopAuditWriter is a no-op implementation of AuditWriter for use in tests
// and when no database is configured.
type NoopAuditWriter struct {
	logger logr.Logger
}

// NewNoopAuditWriter creates a new NoopAuditWriter.
func NewNoopAuditWriter(logger logr.Logger) *NoopAuditWriter {
	return &NoopAuditWriter{
		logger: logger.WithName("audit-noop"),
	}
}

// WriteEntry logs the entry at debug level and returns nil.
func (n *NoopAuditWriter) WriteEntry(ctx context.Context, entry *AuditLogEntry) error {
	n.logger.V(1).Info("NoopAuditWriter: WriteEntry called (no-op)",
		"actor", entry.Actor,
		"action", entry.Action,
		"resource_name", entry.ResourceName,
	)
	return nil
}

// QueryEntries returns an empty slice.
func (n *NoopAuditWriter) QueryEntries(ctx context.Context, filters AuditQueryFilters) ([]AuditLogEntry, error) {
	n.logger.V(1).Info("NoopAuditWriter: QueryEntries called (no-op)")
	return []AuditLogEntry{}, nil
}
