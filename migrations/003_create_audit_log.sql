-- Migration 003: Create audit_log table
-- Stores audit trail for all system and user actions.

CREATE TABLE IF NOT EXISTS audit_log (
    id              BIGSERIAL PRIMARY KEY,
    timestamp       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    actor           TEXT NOT NULL,
    action          TEXT NOT NULL,
    resource_type   TEXT NOT NULL,
    resource_name   TEXT NOT NULL,
    namespace       TEXT,
    details         JSONB,
    reason          TEXT,
    expected_savings NUMERIC(12,6),
    result          TEXT
);

CREATE INDEX idx_audit_log_timestamp ON audit_log(timestamp);
CREATE INDEX idx_audit_log_actor ON audit_log(actor);
CREATE INDEX idx_audit_log_namespace ON audit_log(namespace);
