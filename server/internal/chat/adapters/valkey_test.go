package adapters

import (
	"context"
	"os"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/redis/go-redis/v9"

	"github.com/whatsapp-v2/server/internal/platform/id"
)

func testDeduper(t *testing.T) *Deduper {
	t.Helper()
	addr := os.Getenv("WA_TEST_VALKEY_ADDR")
	if addr == "" {
		t.Skip("WA_TEST_VALKEY_ADDR not set — runs in the CI integration job")
	}
	client := redis.NewClient(&redis.Options{Addr: addr})
	if err := client.Ping(context.Background()).Err(); err != nil {
		t.Fatalf("valkey ping: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return NewDeduper(client)
}

func TestIntegration_Dedupe_ClaimCommitReplay(t *testing.T) {
	d := testDeduper(t)
	ctx := context.Background()
	u := id.New()

	won, _, pending, err := d.Claim(ctx, u)
	if err != nil || !won || pending {
		t.Fatalf("first claim: won=%v pending=%v err=%v", won, pending, err)
	}

	// Before commit, a duplicate sees the pending marker.
	won, _, pending, err = d.Claim(ctx, u)
	if err != nil || won || !pending {
		t.Fatalf("pending claim: won=%v pending=%v err=%v", won, pending, err)
	}

	// After commit, a duplicate gets the committed seq (identical ack).
	if err := d.Commit(ctx, u, 42); err != nil {
		t.Fatal(err)
	}
	won, seq, pending, err := d.Claim(ctx, u)
	if err != nil || won || pending || seq != 42 {
		t.Fatalf("committed claim: won=%v pending=%v seq=%d err=%v", won, pending, seq, err)
	}

	// After release, the id can be claimed fresh again.
	if err := d.Release(ctx, u); err != nil {
		t.Fatal(err)
	}
	won, _, _, err = d.Claim(ctx, u)
	if err != nil || !won {
		t.Fatalf("post-release claim: won=%v err=%v", won, err)
	}
}

// Concurrent claims of the same id: exactly one wins.
func TestIntegration_Dedupe_ConcurrentSingleWinner(t *testing.T) {
	d := testDeduper(t)
	ctx := context.Background()
	u := id.New()

	var wins int64
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if won, _, _, err := d.Claim(ctx, u); err == nil && won {
				atomic.AddInt64(&wins, 1)
			}
		}()
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("concurrent claims produced %d winners, want exactly 1", wins)
	}
}
