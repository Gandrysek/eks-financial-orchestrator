-- Migration 001: Create cost_records hypertable
-- Stores time-series cost data aggregated by namespace, service, team, and pod.

CREATE TABLE IF NOT EXISTS cost_records (
    time            TIMESTAMPTZ NOT NULL,
    namespace       TEXT NOT NULL,
    service         TEXT,
    team            TEXT,
    pod_name        TEXT,
    node_name       TEXT,
    instance_type   TEXT,
    purchase_option TEXT,
    cpu_cost        NUMERIC(12,6),
    memory_cost     NUMERIC(12,6),
    network_cost    NUMERIC(12,6),
    storage_cost    NUMERIC(12,6),
    total_cost      NUMERIC(12,6),
    is_approximate  BOOLEAN DEFAULT FALSE,
    UNIQUE (time, namespace, pod_name)
);

SELECT create_hypertable('cost_records', 'time');

-- Retention policy: automatically drop data older than 90 days
SELECT add_retention_policy('cost_records', INTERVAL '90 days');
