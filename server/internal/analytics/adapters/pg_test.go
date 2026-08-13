package adapters

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/whatsapp-v2/server/internal/analytics/domain"
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("WA_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("WA_TEST_PG_DSN not set — runs in the CI integration job")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("pg connect: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestIntegration_Rollups_IncrSetQueryPurge(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	r := NewRollups(pool)

	// Use a far-past unique day so the test is isolated + easy to clean up.
	day := domain.Day(time.Date(1990, 1, 2, 0, 0, 0, 0, time.UTC))
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM analytics_daily WHERE day = $1`, day) })

	// IncrDaily accumulates.
	if err := r.IncrDaily(ctx, day, "signups", 3); err != nil {
		t.Fatalf("incr: %v", err)
	}
	if err := r.IncrDaily(ctx, day, "signups", 4); err != nil {
		t.Fatalf("incr: %v", err)
	}
	// SetDaily overwrites (DAU/MAU semantics).
	if err := r.SetDaily(ctx, day, "dau", 42); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := r.SetDaily(ctx, day, "dau", 50); err != nil {
		t.Fatalf("set: %v", err)
	}

	rows, err := r.Query(ctx, day, day)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	got := map[string]int64{}
	for _, v := range rows {
		got[v.Metric] = v.Value
	}
	if got["signups"] != 7 {
		t.Errorf("signups = %d, want 7 (accumulated)", got["signups"])
	}
	if got["dau"] != 50 {
		t.Errorf("dau = %d, want 50 (overwritten)", got["dau"])
	}

	// Purge removes rows strictly older than the cutoff.
	n, err := r.PurgeOlderThan(ctx, domain.Day(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if n < 2 {
		t.Errorf("purged %d rows, want ≥2", n)
	}
}
