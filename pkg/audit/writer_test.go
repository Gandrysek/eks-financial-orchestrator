package audit

import (
	"context"
	"testing"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/log"
)

func TestNoopAuditWriter_WriteEntry(t *testing.T) {
	writer := NewNoopAuditWriter(log.Log)
	ctx := context.Background()

	entry := &AuditLogEntry{
		ID:              "test-1",
		Timestamp:       time.Now(),
		Actor:           "system",
		Action:          "policy_create",
		ResourceType:    "FinancialPolicy",
		ResourceName:    "prod-budget",
		Namespace:       "production",
		Details:         map[string]interface{}{"key": "value"},
		Reason:          "initial creation",
		ExpectedSavings: 0,
		Result:          "success",
	}

	err := writer.WriteEntry(ctx, entry)
	if err != nil {
		t.Fatalf("NoopAuditWriter.WriteEntry returned error: %v", err)
	}
}

func TestNoopAuditWriter_QueryEntries(t *testing.T) {
	writer := NewNoopAuditWriter(log.Log)
	ctx := context.Background()

	entries, err := writer.QueryEntries(ctx, AuditQueryFilters{
		Actor:  "system",
		Action: "policy_create",
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("NoopAuditWriter.QueryEntries returned error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected empty slice, got %d entries", len(entries))
	}
}

func TestNoopAuditWriter_QueryEntries_EmptyFilters(t *testing.T) {
	writer := NewNoopAuditWriter(log.Log)
	ctx := context.Background()

	entries, err := writer.QueryEntries(ctx, AuditQueryFilters{})
	if err != nil {
		t.Fatalf("NoopAuditWriter.QueryEntries returned error: %v", err)
	}
	if entries == nil {
		t.Error("expected non-nil empty slice, got nil")
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestDefaultAuditWriter_Constructor(t *testing.T) {
	// We can't test with a real DB connection, but we can verify the
	// constructor doesn't panic and returns a non-nil writer.
	writer := NewDefaultAuditWriter(nil, log.Log)
	if writer == nil {
		t.Fatal("NewDefaultAuditWriter returned nil")
	}
	if writer.db != nil {
		t.Error("expected db to be nil when constructed with nil")
	}
}

func TestNoopAuditWriter_ImplementsInterface(t *testing.T) {
	// Compile-time check that NoopAuditWriter implements AuditWriter.
	var _ AuditWriter = (*NoopAuditWriter)(nil)
}

func TestDefaultAuditWriter_ImplementsInterface(t *testing.T) {
	// Compile-time check that DefaultAuditWriter implements AuditWriter.
	var _ AuditWriter = (*DefaultAuditWriter)(nil)
}
