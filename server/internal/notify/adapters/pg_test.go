package adapters

import (
	"context"
	"crypto/rand"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/whatsapp-v2/server/internal/notify"
	"github.com/whatsapp-v2/server/internal/platform/id"
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("WA_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("WA_TEST_PG_DSN not set — runs in the CI integration job")
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

// seedDeviceWithToken creates a user + device + push token, returning the
// device id.
func seedDeviceWithToken(t *testing.T, pool *pgxpool.Pool, provider notify.Provider, token string) string {
	t.Helper()
	ctx := context.Background()
	userID, deviceID := id.New(), id.New()
	if _, err := pool.Exec(ctx, `INSERT INTO users (id, phone_hash) VALUES ($1,$2)`, userID, rnd(t, 32)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO devices (id, user_id, is_primary, platform, identity_key, cert)
		VALUES ($1,$2,true,1,$3,$3)`, deviceID, userID, rnd(t, 32)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO push_tokens (device_id, provider, token) VALUES ($1,$2,$3)`,
		deviceID, int16(provider), token); err != nil {
		t.Fatal(err)
	}
	return deviceID
}

func TestIntegration_TokenStore_ResolveDelete(t *testing.T) {
	pool := testPool(t)
	store := NewTokenStore(pool)
	ctx := context.Background()

	deviceID := seedDeviceWithToken(t, pool, notify.ProviderNtfy, "https://ntfy.local/up/abc")

	token, provider, err := store.Resolve(ctx, deviceID)
	if err != nil || token != "https://ntfy.local/up/abc" || provider != notify.ProviderNtfy {
		t.Fatalf("resolve: token=%q provider=%v err=%v", token, provider, err)
	}

	if err := store.Delete(ctx, deviceID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Resolve(ctx, deviceID); err != notify.ErrNoToken {
		t.Fatalf("after delete: want ErrNoToken, got %v", err)
	}
}

func TestIntegration_TokenStore_NoToken(t *testing.T) {
	store := NewTokenStore(testPool(t))
	if _, _, err := store.Resolve(context.Background(), id.New()); err != notify.ErrNoToken {
		t.Fatalf("unknown device: want ErrNoToken, got %v", err)
	}
}
