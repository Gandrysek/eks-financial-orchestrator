-- Migration 002: Create forecasts hypertable
-- Stores cost forecast data with confidence intervals.

CREATE TABLE IF NOT EXISTS forecasts (
    time            TIMESTAMPTZ NOT NULL,
    generated_at    TIMESTAMPTZ NOT NULL,
    namespace       TEXT NOT NULL,
    period_days     INTEGER NOT NULL,
    forecasted_cost NUMERIC(12,6),
    lower_bound     NUMERIC(12,6),
    upper_bound     NUMERIC(12,6),
    confidence      NUMERIC(5,4)
);

SELECT create_hypertable('forecasts', 'time');
