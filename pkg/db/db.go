// Package db provides a database connection helper for TimescaleDB/PostgreSQL
// using pgxpool for connection pooling and a simple migration runner.
package db

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DB wraps a pgxpool.Pool and provides helpers for connection management
// and schema migrations.
type DB struct {
	Pool *pgxpool.Pool
}

// NewDB creates a new database connection pool with sensible defaults.
// The connString should be a PostgreSQL connection URI, e.g.
// "postgres://user:pass@host:5432/dbname?sslmode=require".
func NewDB(ctx context.Context, connString string) (*DB, error) {
	cfg, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return nil, fmt.Errorf("parsing connection string: %w", err)
	}

	// Sensible pool defaults for an in-cluster operator workload.
	cfg.MaxConns = 10
	cfg.MinConns = 2
	cfg.HealthCheckPeriod = 30 * time.Second
	cfg.MaxConnLifetime = 30 * time.Minute
	cfg.MaxConnIdleTime = 5 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("creating connection pool: %w", err)
	}

	// Verify connectivity before returning.
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pinging database: %w", err)
	}

	return &DB{Pool: pool}, nil
}

// Close releases all connections in the pool.
func (d *DB) Close() {
	if d.Pool != nil {
		d.Pool.Close()
	}
}

// RunMigrations reads SQL files from migrationsDir and executes them in
// lexicographic order. Each file is executed as a single statement batch
// inside a transaction so that a failed migration does not leave the
// database in a partially-applied state.
func (d *DB) RunMigrations(ctx context.Context, migrationsDir string) error {
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return fmt.Errorf("reading migrations directory %s: %w", migrationsDir, err)
	}

	// Collect and sort .sql files.
	var sqlFiles []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasSuffix(e.Name(), ".sql") {
			sqlFiles = append(sqlFiles, e.Name())
		}
	}
	sort.Strings(sqlFiles)

	for _, name := range sqlFiles {
		path := filepath.Join(migrationsDir, name)
		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading migration file %s: %w", name, err)
		}

		sql := string(content)
		if strings.TrimSpace(sql) == "" {
			continue
		}

		if _, err := d.Pool.Exec(ctx, sql); err != nil {
			return fmt.Errorf("executing migration %s: %w", name, err)
		}
	}

	return nil
}
