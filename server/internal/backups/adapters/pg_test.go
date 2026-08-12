package adapters

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/whatsapp-v2/server/internal/backups"
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
		t.Fatalf("pg connect: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func seedUser(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	uid := id.New()
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO users (id, phone_hash) VALUES ($1, $2)`, uid, []byte("ph-"+uid)); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return uid
}

func TestIntegration_Backups_LifecycleAndOneActive(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	store := NewStore(pool)
	user := seedUser(t, pool)
	now := time.Now()

	// Latest with nothing → ErrNotFound.
	if _, err := store.Latest(ctx, user); !errors.Is(err, backups.ErrNotFound) {
		t.Fatalf("empty latest = %v, want ErrNotFound", err)
	}

	// Create + complete the first backup.
	first := backups.Backup{ID: id.New(), UserID: user, ObjectKey: "backups/" + user + "/1", SizeBytes: 1000, Handle: "h1", CreatedAt: now}
	if err := store.CreatePending(ctx, first); err != nil {
		t.Fatalf("create pending: %v", err)
	}
	if err := store.MarkComplete(ctx, first.ID); err != nil {
		t.Fatalf("mark complete: %v", err)
	}
	if got, err := store.Latest(ctx, user); err != nil || got.ID != first.ID {
		t.Fatalf("latest = %+v (%v), want first", got, err)
	}

	// A second complete backup; OldComplete finds the first for reclaim.
	second := backups.Backup{ID: id.New(), UserID: user, ObjectKey: "backups/" + user + "/2", SizeBytes: 2000, Handle: "h2", CreatedAt: now.Add(time.Minute)}
	_ = store.CreatePending(ctx, second)
	_ = store.MarkComplete(ctx, second.ID)

	old, err := store.OldComplete(ctx, user, second.ID)
	if err != nil || len(old) != 1 || old[0].ID != first.ID {
		t.Fatalf("old complete = %+v (%v), want [first]", old, err)
	}
	// Reclaim the old one; latest is now the second.
	if err := store.Delete(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := store.Latest(ctx, user); got.ID != second.ID {
		t.Fatalf("latest after reclaim = %s, want second", got.ID)
	}
	// The handle is cleared on completion.
	if got, _ := store.Get(ctx, second.ID); got.Handle != "" {
		t.Fatalf("completed handle = %q, want empty", got.Handle)
	}
}
