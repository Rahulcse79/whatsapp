package adapters

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/whatsapp-v2/server/internal/bots"
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

func seedUser(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	uid := id.New()
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO users (id, phone_hash) VALUES ($1, $2)`, uid, []byte("ph-"+uid)); err != nil {
		t.Fatal(err)
	}
	return uid
}

func TestIntegration_Bots(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	s := NewStore(pool)
	owner := seedUser(t, pool)
	handle := "bot" + id.New()[:8]

	b := bots.Bot{ID: id.New(), OwnerID: owner, Handle: handle, Name: "Test", WebhookURL: "https://b.example/h", Secret: "s1", CreatedAt: time.Now()}
	if err := s.Create(ctx, b); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetByHandle(ctx, handle)
	if err != nil || got.ID != b.ID {
		t.Fatalf("get by handle: %v %+v", err, got)
	}
	if err := s.SetSecret(ctx, b.ID, "s2"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(ctx, b.ID); got.Secret != "s2" {
		t.Fatalf("secret not rotated: %q", got.Secret)
	}
	list, _ := s.ListByOwner(ctx, owner)
	if len(list) != 1 {
		t.Fatalf("list: %d", len(list))
	}
	if err := s.Delete(ctx, owner, b.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, b.ID); !errors.Is(err, bots.ErrNotFound) {
		t.Fatal("bot should be gone")
	}
}
