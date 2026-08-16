package adapters

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/whatsapp-v2/server/internal/deviceauth"
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

func TestIntegration_Passkeys(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	s := NewStore(pool)
	user := seedUser(t, pool)

	// challenge round-trip (single-use)
	if err := s.SaveChallenge(ctx, deviceauth.Challenge{Value: "ch1", UserID: user, Purpose: "login", ExpiresAt: time.Now().Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	got, err := s.TakeChallenge(ctx, "ch1")
	if err != nil || got.UserID != user || got.Purpose != "login" {
		t.Fatalf("take challenge: %v %+v", err, got)
	}
	if _, err := s.TakeChallenge(ctx, "ch1"); err != deviceauth.ErrNotFound {
		t.Fatal("challenge must be single-use")
	}

	// credential CRUD
	cred := deviceauth.Credential{ID: "cred-1", UserID: user, Alg: -7, PublicKey: make([]byte, 64), Name: "Touch ID", CreatedAt: time.Now()}
	if err := s.CreateCredential(ctx, cred); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateSignCount(ctx, "cred-1", 5, time.Now()); err != nil {
		t.Fatal(err)
	}
	back, err := s.GetCredential(ctx, "cred-1")
	if err != nil || back.SignCount != 5 || back.LastUsedAt == nil {
		t.Fatalf("get credential: %v %+v", err, back)
	}
	list, _ := s.ListCredentials(ctx, user)
	if len(list) != 1 {
		t.Fatalf("list: %d", len(list))
	}
	if err := s.DeleteCredential(ctx, user, "cred-1"); err != nil {
		t.Fatal(err)
	}
	if l, _ := s.ListCredentials(ctx, user); len(l) != 0 {
		t.Fatal("credential should be gone")
	}

	// login events + known IPs + recent
	for _, e := range []deviceauth.LoginEvent{
		{ID: id.New(), UserID: user, IP: "1.1.1.1", UserAgent: "A", At: time.Now().Add(-2 * time.Minute)},
		{ID: id.New(), UserID: user, IP: "2.2.2.2", UserAgent: "B", At: time.Now(), Suspicious: true},
	} {
		if err := s.RecordLogin(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	ips, _ := s.KnownIPs(ctx, user)
	if len(ips) != 2 {
		t.Fatalf("known IPs: %+v", ips)
	}
	recent, _ := s.RecentLogins(ctx, user, 10)
	if len(recent) != 2 || recent[0].IP != "2.2.2.2" || !recent[0].Suspicious {
		t.Fatalf("recent logins (newest first): %+v", recent)
	}
}
