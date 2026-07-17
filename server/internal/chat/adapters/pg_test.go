package adapters

import (
	"context"
	"crypto/rand"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/whatsapp-v2/server/internal/chat"
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

// seedUser creates a user with n devices, returning the user id and its
// device ids.
func seedUser(t *testing.T, pool *pgxpool.Pool, n int) (string, []string) {
	t.Helper()
	ctx := context.Background()
	userID := id.New()
	if _, err := pool.Exec(ctx, `INSERT INTO users (id, phone_hash) VALUES ($1,$2)`, userID, rnd(t, 32)); err != nil {
		t.Fatal(err)
	}
	var devices []string
	for i := 0; i < n; i++ {
		did := id.New()
		if _, err := pool.Exec(ctx, `
			INSERT INTO devices (id, user_id, is_primary, platform, identity_key, cert)
			VALUES ($1,$2,$3,1,$4,$4)`, did, userID, i == 0, rnd(t, 32)); err != nil {
			t.Fatal(err)
		}
		devices = append(devices, did)
	}
	return userID, devices
}

func TestIntegration_GetOrCreateDirect_Idempotent(t *testing.T) {
	pool := testPool(t)
	store := NewStore(pool)
	ctx := context.Background()
	userA, _ := seedUser(t, pool, 1)
	userB, _ := seedUser(t, pool, 1)

	c1, err := store.GetOrCreateDirect(ctx, userA, userB)
	if err != nil {
		t.Fatal(err)
	}
	// Same pair, reversed order → same conversation.
	c2, err := store.GetOrCreateDirect(ctx, userB, userA)
	if err != nil {
		t.Fatal(err)
	}
	if c1 != c2 {
		t.Fatalf("direct conversation not deduplicated: %s vs %s", c1, c2)
	}
	var members int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM conversation_members WHERE conversation_id=$1`, c1).Scan(&members); err != nil {
		t.Fatal(err)
	}
	if members != 2 {
		t.Fatalf("members = %d, want 2", members)
	}
}

func TestIntegration_Accept_SeqAndFanout(t *testing.T) {
	pool := testPool(t)
	store := NewStore(pool)
	ctx := context.Background()

	userA, devsA := seedUser(t, pool, 2) // sender has 2 devices
	userB, devsB := seedUser(t, pool, 3) // recipient has 3
	conv, err := store.GetOrCreateDirect(ctx, userA, userB)
	if err != nil {
		t.Fatal(err)
	}
	sender := devsA[0]

	now := time.Now()
	res, err := store.Accept(ctx, chat.AcceptParams{
		SenderUserID: userA, SenderDeviceID: sender, ConversationID: conv,
		MsgUUID: id.New(), Kind: chat.KindText, Ciphertext: []byte("sealed"),
		AcceptedAt: now, ExpiresAt: now.Add(chat.InboxTTL),
	})
	if err != nil {
		t.Fatal(err)
	}
	// Recipients = userB's 3 devices + userA's OTHER device (self-sync) = 4.
	wantRecipients := (len(devsB)) + (len(devsA) - 1)
	if res.Seq != 1 || res.RecipientCount != wantRecipients {
		t.Fatalf("seq=%d recipients=%d, want 1 and %d", res.Seq, res.RecipientCount, wantRecipients)
	}

	// The sending device must NOT receive its own message.
	var senderRows int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM message_inbox WHERE conversation_id=$1 AND recipient_device_id=$2`,
		conv, sender).Scan(&senderRows); err != nil {
		t.Fatal(err)
	}
	if senderRows != 0 {
		t.Fatal("sending device received its own message")
	}

	// A second send increments the sequence.
	res2, err := store.Accept(ctx, chat.AcceptParams{
		SenderUserID: userA, SenderDeviceID: sender, ConversationID: conv,
		MsgUUID: id.New(), Kind: chat.KindText, Ciphertext: []byte("sealed2"),
		AcceptedAt: now, ExpiresAt: now.Add(chat.InboxTTL),
	})
	if err != nil || res2.Seq != 2 {
		t.Fatalf("second seq = %d (err %v), want 2", res2.Seq, err)
	}
}

func TestIntegration_Accept_NotMember(t *testing.T) {
	pool := testPool(t)
	store := NewStore(pool)
	ctx := context.Background()

	userA, devsA := seedUser(t, pool, 1)
	userB, _ := seedUser(t, pool, 1)
	stranger, devsS := seedUser(t, pool, 1)
	conv, err := store.GetOrCreateDirect(ctx, userA, userB)
	if err != nil {
		t.Fatal(err)
	}
	_ = devsA

	now := time.Now()
	_, err = store.Accept(ctx, chat.AcceptParams{
		SenderUserID: stranger, SenderDeviceID: devsS[0], ConversationID: conv,
		MsgUUID: id.New(), Kind: chat.KindText, Ciphertext: []byte("x"),
		AcceptedAt: now, ExpiresAt: now.Add(chat.InboxTTL),
	})
	if err != chat.ErrNotMember {
		t.Fatalf("want ErrNotMember, got %v", err)
	}
}
