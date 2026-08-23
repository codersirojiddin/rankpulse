// Package db manages the shared pgx connection pool used across the
// API server and the background worker.
package db

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// NewPool opens a connection pool tuned for Supabase's pgbouncer
// (transaction mode) pooler. Keep MaxConns modest — free-tier Supabase
// projects cap total pooled connections, and Render free-tier services
// don't need a large pool to stay responsive.
func NewPool(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	cfg.MaxConns = 10
	cfg.MinConns = 1
	cfg.MaxConnLifetime = 30 * time.Minute
	cfg.MaxConnIdleTime = 5 * time.Minute
	cfg.HealthCheckPeriod = 1 * time.Minute

	// Supabase's pooled connection string (port 6543) runs PgBouncer in
	// "transaction mode", which does NOT support server-side prepared
	// statements. pgx defaults to preparing every query, which causes
	// random query failures against a pooler like this. Forcing the
	// simple protocol avoids prepared statements entirely.
	cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, err
	}

	return pool, nil
}