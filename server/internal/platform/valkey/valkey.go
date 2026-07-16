// Package valkey builds the client for the cache/ephemeral tier. The binding
// invariant (valkey-keyspace.md): nothing durable lives in Valkey — every
// key is rebuildable or expendable, so client loss policies always fail
// closed rather than fabricate state.
package valkey

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Config for the Valkey connection.
type Config struct {
	Addr     string
	Password string
}

// New connects and verifies liveness with a bounded ping. Callers own
// Close(). go-redis is protocol-compatible with Valkey 8.
func New(ctx context.Context, cfg Config) (*redis.Client, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		// Fail fast: Valkey calls sit on hot paths with tight budgets
		// (200 ms rule in design-patterns §3); queuing behind a dead node
		// is worse than erroring.
		DialTimeout:  2 * time.Second,
		ReadTimeout:  500 * time.Millisecond,
		WriteTimeout: 500 * time.Millisecond,
	})

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("valkey: ping %s: %w", cfg.Addr, err)
	}
	return client, nil
}
