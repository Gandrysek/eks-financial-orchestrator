package audit

import (
	"context"
	"time"
)

// AuditLogEntry represents a single entry in the audit log.
type AuditLogEntry struct {
	ID              string    `json:"id"`
	Timestamp       time.Time `json:"timestamp"`
	Actor           string    `json:"actor"`
	Action          string    `json:"action"`           // policy_create, policy_update, optimization_apply, etc.
	ResourceType    string    `json:"resource_type"`
	ResourceName    string    `json:"resource_name"`
	Namespace       string    `json:"namespace,omitempty"`
	Details         map[string]interface{} `json:"details,omitempty"`
	Reason          string    `json:"reason,omitempty"`
	ExpectedSavings float64   `json:"expected_savings,omitempty"`
	Result          string    `json:"result"` // success, failure, rolled_back
}

// AuditWriter defines the interface for writing and querying audit log entries.
type AuditWriter interface {
	// WriteEntry records an audit log entry to the audit log store
	// and emits a Kubernetes Event.
	WriteEntry(ctx context.Context, entry *AuditLogEntry) error

	// QueryEntries retrieves audit log entries matching the given filters.
	QueryEntries(ctx context.Context, filters AuditQueryFilters) ([]AuditLogEntry, error)
}

// AuditQueryFilters defines filters for querying audit log entries.
type AuditQueryFilters struct {
	Actor        string    `json:"actor,omitempty"`
	Action       string    `json:"action,omitempty"`
	ResourceType string    `json:"resource_type,omitempty"`
	Namespace    string    `json:"namespace,omitempty"`
	StartTime    time.Time `json:"start_time,omitempty"`
	EndTime      time.Time `json:"end_time,omitempty"`
	Limit        int       `json:"limit,omitempty"`
}
