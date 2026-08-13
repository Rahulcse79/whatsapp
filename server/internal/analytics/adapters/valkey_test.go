package adapters

import (
	"context"
	"os"
	"testing"

	"github.com/redis/go-redis/v9"
)

func testClient(t *testing.T) *redis.Client {
	t.Helper()
	addr := os.Getenv("WA_TEST_VALKEY_ADDR")
	if addr == "" {
		t.Skip("WA_TEST_VALKEY_ADDR not set — runs in the CI integration job")
	}
	c := redis.NewClient(&redis.Options{Addr: addr})
	if err := c.Ping(context.Background()).Err(); err != nil {
		t.Fatalf("valkey ping: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestIntegration_Distinct_HLLCountAndUnion(t *testing.T) {
	c := testClient(t)
	ctx := context.Background()
	d := NewDistinct(c)

	b1, b2 := "test_active:day1", "test_active:day2"
	t.Cleanup(func() { c.Del(ctx, "analytics:hll:"+b1, "analytics:hll:"+b2) })

	// day1: u1, u2, u2 (dup) → 2 distinct.
	for _, u := range []string{"u1", "u2", "u2"} {
		if err := d.Add(ctx, b1, u); err != nil {
			t.Fatalf("add: %v", err)
		}
	}
	// day2: u2 (overlap), u3.
	for _, u := range []string{"u2", "u3"} {
		if err := d.Add(ctx, b2, u); err != nil {
			t.Fatalf("add: %v", err)
		}
	}

	if n, err := d.Count(ctx, b1); err != nil || n != 2 {
		t.Fatalf("day1 count = %d (err %v), want 2", n, err)
	}
	// Union across both buckets: {u1,u2,u3} = 3 (u2 counted once).
	if n, err := d.Count(ctx, b1, b2); err != nil || n != 3 {
		t.Fatalf("union count = %d (err %v), want 3", n, err)
	}
	// Empty bucket list → 0, no error.
	if n, err := d.Count(ctx); err != nil || n != 0 {
		t.Fatalf("empty count = %d (err %v), want 0", n, err)
	}
}
