-- Migration 004: Create shared_cost_allocation hypertable
-- Stores shared cluster cost allocations distributed across namespaces.

CREATE TABLE IF NOT EXISTS shared_cost_allocation (
    time            TIMESTAMPTZ NOT NULL,
    namespace       TEXT NOT NULL,
    cost_category   TEXT NOT NULL,
    allocated_cost  NUMERIC(12,6),
    allocation_basis TEXT
);

SELECT create_hypertable('shared_cost_allocation', 'time');
