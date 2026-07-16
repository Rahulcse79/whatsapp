// Package pg builds the PostgreSQL connection pool. Statement timeouts are
// set at the session level so no query can exceed its budget
// (design-patterns doc §3); pool sizes come from per-deployable budgets —
// the bulkhead that keeps one noisy path from starving the others.
package pg

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Config for the pool; zero values fall back to pgx defaults.
type Config struct {
	DSN              string
	MaxConns         int32
	MinConns         int32
	StatementTimeout time.Duration
}

// NewPool connects, applies the pool budget and statement timeout, and
// verifies liveness with a bounded ping. The returned pool is safe for
// concurrent use; callers own Close().
func NewPool(ctx context.Context, cfg Config) (*pgxpool.Pool, error) {
	pc, err := pgxpool.ParseConfig(cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("pg: parsing DSN: %w", err)
	}
	if cfg.MaxConns > 0 {
		pc.MaxConns = cfg.MaxConns
	}
	if cfg.MinConns > 0 {
		pc.MinConns = cfg.MinConns
	}
	if cfg.StatementTimeout > 0 {
		pc.ConnConfig.RuntimeParams["statement_timeout"] =
			strconv.FormatInt(cfg.StatementTimeout.Milliseconds(), 10)
	}

	pool, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		return nil, fmt.Errorf("pg: creating pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pg: ping: %w", err)
	}
	return pool, nil
}
