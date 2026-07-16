package adapters

import (
	"context"
	"crypto/rand"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/whatsapp-v2/server/internal/devices"
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

// seedUserDevice inserts a user + one (primary) device and returns their ids.
func seedUserDevice(t *testing.T, pool *pgxpool.Pool, primary bool) (string, string) {
	t.Helper()
	ctx := context.Background()
	userID, deviceID := id.New(), id.New()
	if _, err := pool.Exec(ctx, `INSERT INTO users (id, phone_hash) VALUES ($1,$2)`, userID, rnd(t, 32)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO devices (id, user_id, is_primary, platform, identity_key, cert)
		VALUES ($1,$2,$3,1,$4,$4)`, deviceID, userID, primary, rnd(t, 32)); err != nil {
		t.Fatal(err)
	}
	return userID, deviceID
}

func TestIntegration_RevokeDevice_AtomicTeardown(t *testing.T) {
	pool := testPool(t)
	store := NewStore(pool)
	ctx := context.Background()
	userID, deviceID := seedUserDevice(t, pool, true)

	// Attach a session, prekeys, and a push token to the device.
	if _, err := pool.Exec(ctx, `
		INSERT INTO sessions (id, device_id, refresh_hash, expires_at)
		VALUES ($1,$2,$3, now() + interval '1 day')`, id.New(), deviceID, rnd(t, 32)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO signed_prekeys (device_id, key_id, pubkey, signature)
		VALUES ($1,1,$2,$3)`, deviceID, rnd(t, 32), rnd(t, 64)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO prekeys (device_id, key_id, pubkey) VALUES ($1,1,$2)`,
		deviceID, rnd(t, 32)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO push_tokens (device_id, provider, token) VALUES ($1,0,'tok')`,
		deviceID); err != nil {
		t.Fatal(err)
	}

	ok, err := store.RevokeDevice(ctx, userID, deviceID)
	if err != nil || !ok {
		t.Fatalf("revoke: ok=%v err=%v", ok, err)
	}

	// Everything bound to the device is gone or revoked.
	assertCount(t, pool, `SELECT count(*) FROM devices WHERE id=$1 AND revoked_at IS NOT NULL`, deviceID, 1)
	assertCount(t, pool, `SELECT count(*) FROM sessions WHERE device_id=$1 AND revoked_at IS NULL`, deviceID, 0)
	assertCount(t, pool, `SELECT count(*) FROM prekeys WHERE device_id=$1`, deviceID, 0)
	assertCount(t, pool, `SELECT count(*) FROM signed_prekeys WHERE device_id=$1`, deviceID, 0)
	assertCount(t, pool, `SELECT count(*) FROM push_tokens WHERE device_id=$1`, deviceID, 0)

	// Idempotent: a second revoke reports nothing to do.
	ok, err = store.RevokeDevice(ctx, userID, deviceID)
	if err != nil || ok {
		t.Fatalf("second revoke: want (false,nil), got (%v,%v)", ok, err)
	}
	// Foreign device: not revocable.
	otherUser, _ := seedUserDevice(t, pool, true)
	if ok, _ := store.RevokeDevice(ctx, otherUser, deviceID); ok {
		t.Fatal("revoked a device belonging to another user")
	}
}

func TestIntegration_LinkApproveFlow(t *testing.T) {
	pool := testPool(t)
	store := NewStore(pool)
	ctx := context.Background()
	userID, primaryID := seedUserDevice(t, pool, true)

	token := id.New()
	now := time.Now()
	if err := store.CreateLink(ctx, devices.Link{
		Token: token, Platform: devices.PlatformWeb, Name: "Laptop",
		IdentityKey: rnd(t, 32), State: devices.LinkPending,
		CreatedAt: now, ExpiresAt: now.Add(devices.LinkTTL),
	}); err != nil {
		t.Fatal(err)
	}

	newDeviceID := id.New()
	if err := store.ApproveLink(ctx, devices.ApproveParams{
		Token: token, UserID: userID, NewDeviceID: newDeviceID, ApprovedBy: primaryID,
		Platform: devices.PlatformWeb, Name: "Laptop", IdentityKey: rnd(t, 32),
		Cert: rnd(t, 64), Now: now,
	}); err != nil {
		t.Fatal(err)
	}

	// The linked device exists, non-primary.
	got, err := store.Get(ctx, newDeviceID)
	if err != nil || got.IsPrimary || got.UserID != userID {
		t.Fatalf("linked device wrong: %+v err=%v", got, err)
	}
	link, _ := store.GetLink(ctx, token)
	if link.State != devices.LinkApproved || link.DeviceID != newDeviceID {
		t.Fatalf("link not approved: %+v", link)
	}

	// A second approval on the same (now non-pending) link is refused.
	if err := store.ApproveLink(ctx, devices.ApproveParams{
		Token: token, UserID: userID, NewDeviceID: id.New(), ApprovedBy: primaryID,
		Platform: devices.PlatformWeb, IdentityKey: rnd(t, 32), Cert: rnd(t, 64), Now: now,
	}); err != devices.ErrNotFound {
		t.Fatalf("double approve: want ErrNotFound, got %v", err)
	}

	if err := store.ConsumeLink(ctx, token); err != nil {
		t.Fatal(err)
	}
	link, _ = store.GetLink(ctx, token)
	if link.State != devices.LinkConsumed {
		t.Fatal("link not consumed")
	}
}

func assertCount(t *testing.T, pool *pgxpool.Pool, sql, arg string, want int) {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), sql, arg).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != want {
		t.Fatalf("%s → %d, want %d", sql, n, want)
	}
}
