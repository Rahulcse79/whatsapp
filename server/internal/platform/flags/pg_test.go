package flags

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
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

func TestIntegration_PGStore_GetList(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	store := NewPGStore(pool)

	flag := "test_flag_" + randSuffix()
	if _, err := pool.Exec(ctx,
		`INSERT INTO feature_flags (flag, rules, updated_by) VALUES ($1, $2::jsonb, $3)`,
		flag, `{"enabled":true,"rollout":25,"allow":["u1"]}`, "oidc|op"); err != nil {
		t.Fatalf("seed flag: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM feature_flags WHERE flag = $1`, flag) })

	rule, present, err := store.Get(ctx, flag)
	if err != nil || !present {
		t.Fatalf("get: rule=%+v present=%v err=%v", rule, present, err)
	}
	if !rule.Enabled || rule.Rollout != 25 || len(rule.Allow) != 1 || rule.Allow[0] != "u1" {
		t.Fatalf("round-trip rule = %+v", rule)
	}

	// Absent flag → present=false, no error.
	if _, present, err := store.Get(ctx, "does_not_exist_"+randSuffix()); err != nil || present {
		t.Fatalf("absent flag: present=%v err=%v", present, err)
	}

	list, err := store.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	found := false
	for _, n := range list {
		if n.Flag == flag {
			found = true
			if n.UpdatedBy != "oidc|op" {
				t.Errorf("updated_by = %q, want oidc|op (text column, migration 000016)", n.UpdatedBy)
			}
		}
	}
	if !found {
		t.Error("seeded flag absent from List")
	}
}

func randSuffix() string {
	return strconv.FormatInt(time.Now().UnixNano(), 36)
}
