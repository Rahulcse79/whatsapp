package adapters

import (
	"context"
	"crypto/rand"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/auth/domain"
	"github.com/whatsapp-v2/server/internal/platform/id"
)

// Integration tests run wherever a migrated database is available — the CI
// migrations job sets WA_TEST_PG_DSN after applying all migrations.
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

func randBytes(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return b
}

func TestIntegration_ChallengeRoundTrip(t *testing.T) {
	store := NewStore(testPool(t))
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)

	ch := domain.Challenge{
		ID:        id.New(),
		PhoneHash: randBytes(t, 32),
		CodeHash:  randBytes(t, 32),
		Salt:      randBytes(t, 16),
		Channel:   domain.ChannelMock,
		CreatedAt: now,
		ExpiresAt: now.Add(10 * time.Minute),
	}
	if err := store.Challenges.Create(ctx, ch); err != nil {
		t.Fatal(err)
	}
	got, err := store.Challenges.Get(ctx, ch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Attempts != 0 || !got.VerifiedAt.IsZero() || got.Channel != domain.ChannelMock {
		t.Fatalf("roundtrip mismatch: %+v", got)
	}
	if err := store.Challenges.IncrementAttempts(ctx, ch.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.Challenges.MarkVerified(ctx, ch.ID, true, now); err != nil {
		t.Fatal(err)
	}
	got, _ = store.Challenges.Get(ctx, ch.ID)
	if got.Attempts != 1 || got.VerifiedAt.IsZero() || !got.PinPending {
		t.Fatalf("mutations not persisted: %+v", got)
	}
	if _, err := store.Challenges.Get(ctx, id.New()); err != auth.ErrNotFound {
		t.Fatalf("missing challenge: want ErrNotFound, got %v", err)
	}
}

func TestIntegration_RegisterRotateReuse(t *testing.T) {
	store := NewStore(testPool(t))
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	phone := randBytes(t, 32)

	reg := func() (auth.RegisterDeviceResult, auth.RegisterDeviceParams) {
		p := auth.RegisterDeviceParams{
			UserID: id.New(), PhoneHash: phone, DeviceID: id.New(),
			Platform: 1, DeviceName: "test", IdentityKey: randBytes(t, 32),
			SessionID: id.New(), RefreshHash: randBytes(t, 32),
			SessionExpiresAt: now.Add(24 * time.Hour), Now: now,
		}
		res, err := store.Registrar.RegisterDevice(ctx, p)
		if err != nil {
			t.Fatal(err)
		}
		return res, p
	}

	res1, p1 := reg()
	if !res1.NewUser {
		t.Fatal("first registration must create the user")
	}
	res2, _ := reg()
	if res2.NewUser || res2.UserID != res1.UserID {
		t.Fatalf("second device must join the same user: %+v vs %+v", res1, res2)
	}

	// Session lookup joins through devices for the user id.
	sess, err := store.Sessions.ByRefreshHash(ctx, p1.RefreshHash)
	if err != nil || sess.UserID != res1.UserID || sess.DeviceID != p1.DeviceID {
		t.Fatalf("session lookup: %+v err=%v", sess, err)
	}

	// Rotate; the old hash becomes the reuse marker.
	newHash := randBytes(t, 32)
	ok, err := store.Sessions.Rotate(ctx, sess.ID, p1.RefreshHash, newHash, now)
	if err != nil || !ok {
		t.Fatalf("rotate: ok=%v err=%v", ok, err)
	}
	if ok, _ := store.Sessions.Rotate(ctx, sess.ID, p1.RefreshHash, randBytes(t, 32), now); ok {
		t.Fatal("second rotate with the stale hash must fail")
	}
	old, err := store.Sessions.ByRotatedFrom(ctx, p1.RefreshHash)
	if err != nil || old.ID != sess.ID {
		t.Fatalf("rotated_from lookup: %+v err=%v", old, err)
	}
	if err := store.Sessions.Revoke(ctx, sess.ID, now); err != nil {
		t.Fatal(err)
	}
	got, _ := store.Sessions.ByRefreshHash(ctx, newHash)
	if got.RevokedAt.IsZero() {
		t.Fatal("revocation not persisted")
	}
}

func TestIntegration_DeviceLimit(t *testing.T) {
	store := NewStore(testPool(t))
	ctx := context.Background()
	now := time.Now().UTC()
	phone := randBytes(t, 32)

	var lastErr error
	for i := 0; i < maxDevicesPerUser+1; i++ {
		_, lastErr = store.Registrar.RegisterDevice(ctx, auth.RegisterDeviceParams{
			UserID: id.New(), PhoneHash: phone, DeviceID: id.New(),
			Platform: 0, IdentityKey: randBytes(t, 32),
			SessionID: id.New(), RefreshHash: randBytes(t, 32),
			SessionExpiresAt: now.Add(time.Hour), Now: now,
		})
		if i < maxDevicesPerUser && lastErr != nil {
			t.Fatalf("device %d rejected early: %v", i+1, lastErr)
		}
	}
	if lastErr != auth.ErrDeviceLimit {
		t.Fatalf("device %d: want ErrDeviceLimit, got %v", maxDevicesPerUser+1, lastErr)
	}
}
