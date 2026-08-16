package adapters

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

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

func TestIntegration_EphemeralTimerAndPurge(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	s := NewStore(pool)

	member := seedUser(t, pool)
	convID := id.New()
	if _, err := pool.Exec(ctx, `INSERT INTO conversations (id, kind) VALUES ($1, 0)`, convID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO conversation_members (conversation_id, user_id) VALUES ($1, $2)`, convID, member); err != nil {
		t.Fatal(err)
	}

	// membership
	if ok, err := s.IsMember(ctx, convID, member); err != nil || !ok {
		t.Fatalf("member should be true: %v %v", ok, err)
	}
	if ok, _ := s.IsMember(ctx, convID, id.New()); ok {
		t.Fatal("stranger should not be a member")
	}

	// get default 0, then set + get
	if ttl, err := s.GetTimer(ctx, convID); err != nil || ttl != 0 {
		t.Fatalf("default timer: %v %d", err, ttl)
	}
	if err := s.SetTimer(ctx, convID, 3600, member, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTimer(ctx, convID, 60, member, time.Now()); err != nil { // upsert
		t.Fatal(err)
	}
	if ttl, err := s.GetTimer(ctx, convID); err != nil || ttl != 60 {
		t.Fatalf("timer after upsert: %v %d", err, ttl)
	}

	// an old inbox row for this conversation is purged past the 60s timer
	old := time.Now().Add(-10 * time.Minute)
	if _, err := pool.Exec(ctx,
		`INSERT INTO message_inbox (recipient_device_id, conversation_id, seq, msg_uuid, sender_device_id, kind, ciphertext, accepted_at, expires_at)
		 VALUES ($1, $2, 1, $3, $4, 1, $5, $6, $7)`,
		id.New(), convID, id.New(), id.New(), []byte("ct"), old, old.Add(30*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	n, err := s.PurgeExpired(ctx, time.Now())
	if err != nil || n != 1 {
		t.Fatalf("purge = (%d, %v), want (1, nil)", n, err)
	}
}
