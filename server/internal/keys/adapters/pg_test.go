package adapters

import (
	"context"
	"crypto/rand"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/whatsapp-v2/server/internal/keys"
	"github.com/whatsapp-v2/server/internal/platform/id"
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("WA_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("WA_TEST_PG_DSN not set — runs in the CI migrations job")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func rnd(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return b
}

// seedDevice creates a user + one device with a signed prekey and n one-time
// prekeys, returning the user and device ids.
func seedDevice(t *testing.T, pool *pgxpool.Pool, store *PrekeyStore, n int) (string, string) {
	t.Helper()
	ctx := context.Background()
	userID, deviceID := id.New(), id.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, phone_hash) VALUES ($1, $2)`, userID, rnd(t, 32)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO devices (id, user_id, is_primary, platform, identity_key, cert)
		VALUES ($1, $2, true, 1, $3, $3)`, deviceID, userID, rnd(t, 32)); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceSignedPrekey(ctx, deviceID,
		keys.SignedPrekey{KeyID: 1, Pubkey: rnd(t, 32), Signature: rnd(t, 64)}); err != nil {
		t.Fatal(err)
	}
	otps := make([]keys.OneTimePrekey, n)
	for i := range otps {
		otps[i] = keys.OneTimePrekey{KeyID: int32(i + 1), Pubkey: rnd(t, 32)}
	}
	if err := store.AddOneTimePrekeys(ctx, deviceID, otps); err != nil {
		t.Fatal(err)
	}
	return userID, deviceID
}

// TestIntegration_ConsumeBundle_ConcurrentDistinct is the T0.09 headline
// guarantee: under concurrent fetches, no one-time prekey is handed out
// twice, and each is consumed exactly once.
func TestIntegration_ConsumeBundle_ConcurrentDistinct(t *testing.T) {
	pool := testPool(t)
	store := NewPrekeyStore(pool)
	const n = 50
	userID, deviceID := seedDevice(t, pool, store, n)
	ctx := context.Background()

	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		seen = map[int32]int{}
		got  int
	)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			bundles, err := store.ConsumeBundle(ctx, userID)
			if err != nil {
				t.Errorf("consume: %v", err)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			for _, b := range bundles {
				if b.OneTimePrekey != nil {
					seen[b.OneTimePrekey.KeyID]++
					got++
				}
			}
		}()
	}
	wg.Wait()

	if got != n {
		t.Fatalf("consumed %d one-time prekeys across %d fetches, want %d", got, n, n)
	}
	for kid, c := range seen {
		if c != 1 {
			t.Fatalf("one-time prekey %d handed out %d times (must be exactly 1)", kid, c)
		}
	}
	// Pool is now exhausted: the next fetch returns a bundle with no OTP.
	bundles, err := store.ConsumeBundle(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	if len(bundles) != 1 || bundles[0].OneTimePrekey != nil {
		t.Fatalf("exhausted pool should yield a bundle without a one-time prekey: %+v", bundles)
	}
	if avail, _ := store.CountAvailable(ctx, deviceID); avail != 0 {
		t.Fatalf("available = %d after exhaustion, want 0", avail)
	}
}

func TestIntegration_ConsumeBundle_NoDevices(t *testing.T) {
	store := NewPrekeyStore(testPool(t))
	if _, err := store.ConsumeBundle(context.Background(), id.New()); err != keys.ErrNoDevices {
		t.Fatalf("want ErrNoDevices, got %v", err)
	}
}

func TestIntegration_Publish_Idempotent(t *testing.T) {
	pool := testPool(t)
	store := NewPrekeyStore(pool)
	_, deviceID := seedDevice(t, pool, store, 3)
	ctx := context.Background()

	// Re-uploading the same one-time prekeys must not duplicate them.
	dup := []keys.OneTimePrekey{{KeyID: 1, Pubkey: rnd(t, 32)}, {KeyID: 2, Pubkey: rnd(t, 32)}}
	if err := store.AddOneTimePrekeys(ctx, deviceID, dup); err != nil {
		t.Fatal(err)
	}
	if avail, _ := store.CountAvailable(ctx, deviceID); avail != 3 {
		t.Fatalf("available = %d, want 3 (duplicate key_ids ignored)", avail)
	}

	// Rotating the signed prekey to a higher key_id updates the current one.
	if err := store.ReplaceSignedPrekey(ctx, deviceID,
		keys.SignedPrekey{KeyID: 2, Pubkey: rnd(t, 32), Signature: rnd(t, 64)}); err != nil {
		t.Fatal(err)
	}
	_ = time.Now
}
