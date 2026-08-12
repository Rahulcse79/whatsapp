package adapters

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/whatsapp-v2/server/internal/contacts"
	"github.com/whatsapp-v2/server/internal/contacts/domain"
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

// tag yields a short unique suffix so usernames/hashes don't collide across runs
// against a persistent database.
func tag(t *testing.T) string { return hex.EncodeToString(rnd(t, 4)) }

// seedUser inserts an active user with the given username (empty → NULL) and
// phone hash, returning the user id.
func seedUser(t *testing.T, pool *pgxpool.Pool, username string, phoneHash []byte) string {
	t.Helper()
	uid := id.New()
	var name any
	if username != "" {
		name = username
	}
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO users (id, phone_hash, username, status) VALUES ($1, $2, $3, 0)`,
		uid, phoneHash, name); err != nil {
		t.Fatal(err)
	}
	return uid
}

func TestIntegration_MatchHashes(t *testing.T) {
	pool := testPool(t)
	st := NewStore(pool)
	ctx := context.Background()

	ph1, ph2, phMiss := rnd(t, 32), rnd(t, 32), rnd(t, 32)
	u1 := seedUser(t, pool, "alice_"+tag(t), ph1)
	u2 := seedUser(t, pool, "", ph2) // registered but no username set

	matches, err := st.MatchHashes(ctx, [][]byte{ph1, ph2, phMiss})
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 2 {
		t.Fatalf("got %d matches, want 2 (phMiss must not match)", len(matches))
	}
	byUser := map[string]contacts.Match{}
	for _, m := range matches {
		byUser[m.UserID] = m
	}
	if !bytes.Equal(byUser[u1].Hash, ph1) {
		t.Error("MatchHashes must echo the exact queried hash so the caller can map it back")
	}
	if byUser[u2].Username != "" {
		t.Errorf("u2 has no username; got %q", byUser[u2].Username)
	}
}

func TestIntegration_MatchHashes_SkipsInactive(t *testing.T) {
	pool := testPool(t)
	st := NewStore(pool)
	ctx := context.Background()

	ph := rnd(t, 32)
	uid := id.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, phone_hash, status) VALUES ($1, $2, 1)`, uid, ph); err != nil { // 1 = suspended
		t.Fatal(err)
	}
	matches, err := st.MatchHashes(ctx, [][]byte{ph})
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("suspended user must not be discoverable; got %d", len(matches))
	}
}

func TestIntegration_SearchUsername(t *testing.T) {
	pool := testPool(t)
	st := NewStore(pool)
	ctx := context.Background()

	tg := tag(t)
	seedUser(t, pool, "alice"+tg, rnd(t, 32))
	seedUser(t, pool, "alicia"+tg, rnd(t, 32))
	seedUser(t, pool, "bob"+tg, rnd(t, 32))

	res, err := st.SearchUsername(ctx, "alic"+tg, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 {
		t.Fatalf("got %d results, want 2 (alice+alicia, not bob): %+v", len(res), res)
	}
}

func TestIntegration_SearchUsername_EscapesWildcard(t *testing.T) {
	pool := testPool(t)
	st := NewStore(pool)
	ctx := context.Background()

	tg := tag(t)
	seedUser(t, pool, "zoe"+tg, rnd(t, 32))

	// A '%' in the query must be matched literally, not as a wildcard.
	res, err := st.SearchUsername(ctx, "z%"+tg, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 0 {
		t.Fatalf("literal %% must not act as a wildcard; got %+v", res)
	}
}

func TestIntegration_UpsertMatched(t *testing.T) {
	pool := testPool(t)
	st := NewStore(pool)
	ctx := context.Background()

	owner := seedUser(t, pool, "owner"+tag(t), rnd(t, 32))
	target := seedUser(t, pool, "targ"+tag(t), rnd(t, 32))
	target2 := seedUser(t, pool, "targ2"+tag(t), rnd(t, 32))
	h := rnd(t, 32)

	if err := st.UpsertMatched(ctx, owner, []contacts.ContactEdge{{Hash: h, MatchedUserID: target}}); err != nil {
		t.Fatal(err)
	}
	// Re-sync updates matched_user_id for the same (owner, hash).
	if err := st.UpsertMatched(ctx, owner, []contacts.ContactEdge{{Hash: h, MatchedUserID: target2}}); err != nil {
		t.Fatal(err)
	}
	var matched string
	if err := pool.QueryRow(ctx,
		`SELECT matched_user_id::text FROM contacts WHERE owner_id = $1 AND contact_phone_hash = $2`,
		owner, h).Scan(&matched); err != nil {
		t.Fatal(err)
	}
	if matched != target2 {
		t.Errorf("matched_user_id = %s, want updated %s", matched, target2)
	}
}

func TestIntegration_Favorites(t *testing.T) {
	pool := testPool(t)
	st := NewStore(pool)
	ctx := context.Background()

	owner := seedUser(t, pool, "owner"+tag(t), rnd(t, 32))
	target := seedUser(t, pool, "targ"+tag(t), rnd(t, 32))

	// Unknown target → FK violation surfaces as ErrNotFound.
	if err := st.Add(ctx, owner, id.New()); !errors.Is(err, contacts.ErrNotFound) {
		t.Fatalf("unknown target: want ErrNotFound, got %v", err)
	}
	// Add is idempotent.
	if err := st.Add(ctx, owner, target); err != nil {
		t.Fatal(err)
	}
	if err := st.Add(ctx, owner, target); err != nil {
		t.Fatalf("second add must be a no-op, got %v", err)
	}
	list, err := st.List(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].UserID != target {
		t.Fatalf("List = %+v, want [%s]", list, target)
	}
	// Remove is idempotent.
	if err := st.Remove(ctx, owner, target); err != nil {
		t.Fatal(err)
	}
	if err := st.Remove(ctx, owner, target); err != nil {
		t.Fatalf("second remove must be a no-op, got %v", err)
	}
	if list, _ = st.List(ctx, owner); len(list) != 0 {
		t.Fatalf("List after remove = %+v, want empty", list)
	}
}

func TestIntegration_Invites(t *testing.T) {
	pool := testPool(t)
	st := NewStore(pool)
	ctx := context.Background()

	inviter := seedUser(t, pool, "inv"+tag(t), rnd(t, 32))
	tok := "tok_" + hex.EncodeToString(rnd(t, 8))

	inv := domain.Invite{Token: tok, InviterID: inviter, ExpiresAt: time.Now().Add(time.Hour).UTC(), MaxUses: 5}
	if err := st.Create(ctx, inv); err != nil {
		t.Fatal(err)
	}
	got, err := st.Get(ctx, tok)
	if err != nil {
		t.Fatal(err)
	}
	if got.InviterID != inviter || got.MaxUses != 5 || got.Uses != 0 || got.RevokedAt != nil {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	// Unknown token → ErrNotFound.
	if _, err := st.Get(ctx, "nope"); !errors.Is(err, contacts.ErrNotFound) {
		t.Fatalf("unknown Get: want ErrNotFound, got %v", err)
	}
	// Revoke by non-owner → ErrNotFound (does not touch the row).
	if err := st.Revoke(ctx, id.New(), tok); !errors.Is(err, contacts.ErrNotFound) {
		t.Fatalf("non-owner revoke: want ErrNotFound, got %v", err)
	}
	// Revoke by owner → sets revoked_at.
	if err := st.Revoke(ctx, inviter, tok); err != nil {
		t.Fatal(err)
	}
	if got, _ = st.Get(ctx, tok); got.RevokedAt == nil {
		t.Error("revoked_at should be set after owner revoke")
	}
	// Revoke an unknown token → ErrNotFound.
	if err := st.Revoke(ctx, inviter, "ghost"); !errors.Is(err, contacts.ErrNotFound) {
		t.Fatalf("unknown revoke: want ErrNotFound, got %v", err)
	}
}
