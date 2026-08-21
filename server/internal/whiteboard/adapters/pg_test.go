package adapters

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/whatsapp-v2/server/internal/platform/id"
	"github.com/whatsapp-v2/server/internal/whiteboard"
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

func TestIntegration_BoardOpLog(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	s := NewStore(pool)
	alice := seedUser(t, pool)
	convID := id.New()
	if _, err := pool.Exec(ctx, `INSERT INTO conversations (id, kind) VALUES ($1, 0)`, convID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO conversation_members (conversation_id, user_id) VALUES ($1, $2)`, convID, alice); err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.IsMember(ctx, convID, alice); !ok {
		t.Fatal("alice should be a member")
	}

	mk := func(opID string, seq int64) whiteboard.Op {
		return whiteboard.Op{ID: opID, ConversationID: convID, Author: alice, Seq: seq, Kind: "stroke", Data: json.RawMessage([]byte(`{"t":"stroke","id":"` + opID + `"}`))}
	}
	if err := s.AppendOp(ctx, mk("s1", 1)); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendOp(ctx, mk("s1", 1)); err != nil { // idempotent retry
		t.Fatal(err)
	}
	if err := s.AppendOp(ctx, mk("s2", 2)); err != nil {
		t.Fatal(err)
	}

	ops, err := s.ListOps(ctx, convID, 0, 100)
	if err != nil || len(ops) != 2 {
		t.Fatalf("list from 0: %v (%d)", err, len(ops))
	}
	inc, _ := s.ListOps(ctx, convID, 1, 100)
	if len(inc) != 1 || inc[0].ID != "s2" {
		t.Fatalf("incremental: %+v", inc)
	}
	if m, _ := s.MaxSeq(ctx, convID); m != 2 {
		t.Fatalf("max seq = %d, want 2", m)
	}
}
