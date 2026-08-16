package adapters

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/whatsapp-v2/server/internal/breakout"
	"github.com/whatsapp-v2/server/internal/breakout/domain"
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

func TestIntegration_LiveSessionLifecycle(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	s := NewStore(pool)
	host := seedUser(t, pool)
	now := time.Now()

	sess := breakout.Session{ID: id.New(), HostID: host, MainRoom: "main-" + id.New(), CreatedAt: now}
	if err := s.CreateSession(ctx, sess); err != nil {
		t.Fatal(err)
	}

	// breakout rooms + assignment + counts
	roomA := breakout.Room{ID: id.New(), SessionID: sess.ID, Name: "Team A", Room: "bo-a", CreatedAt: now}
	if err := s.CreateRoom(ctx, roomA); err != nil {
		t.Fatal(err)
	}
	if err := s.SetAssignment(ctx, sess.ID, "11111111-1111-1111-1111-111111111111", &roomA.ID, now); err != nil {
		t.Fatal(err)
	}
	counts, err := s.CountByRoom(ctx, sess.ID)
	if err != nil || counts[roomA.ID] != 1 {
		t.Fatalf("counts: %v %+v", err, counts)
	}
	got, err := s.GetAssignment(ctx, sess.ID, "11111111-1111-1111-1111-111111111111")
	if err != nil || got.RoomID == nil || *got.RoomID != roomA.ID {
		t.Fatalf("assignment: %v %+v", err, got)
	}
	// reassign to main (nil)
	if err := s.SetAssignment(ctx, sess.ID, "11111111-1111-1111-1111-111111111111", nil, now); err != nil {
		t.Fatal(err)
	}
	got2, _ := s.GetAssignment(ctx, sess.ID, "11111111-1111-1111-1111-111111111111")
	if got2.RoomID != nil {
		t.Fatalf("reassign to main: %+v", got2)
	}

	// close rooms → open list empty
	if err := s.CloseRooms(ctx, sess.ID, now); err != nil {
		t.Fatal(err)
	}
	open, _ := s.ListRooms(ctx, sess.ID)
	if len(open) != 0 {
		t.Fatalf("rooms should be closed: %+v", open)
	}

	// egress + recording state
	if err := s.SetEgress(ctx, sess.ID, domain.EgressLive, domain.EgressRTMP, "rtmp://x/y", "egr-1"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetRecording(ctx, sess.ID, domain.RecordingActive); err != nil {
		t.Fatal(err)
	}
	reloaded, err := s.GetSession(ctx, sess.ID)
	if err != nil || reloaded.EgressState != domain.EgressLive || reloaded.EgressRef != "egr-1" || reloaded.Recording != domain.RecordingActive {
		t.Fatalf("session state: %v %+v", err, reloaded)
	}

	// consent: set, list, reset
	if err := s.SetConsent(ctx, sess.ID, "22222222-2222-2222-2222-222222222222", true, now); err != nil {
		t.Fatal(err)
	}
	if err := s.SetConsent(ctx, sess.ID, "22222222-2222-2222-2222-222222222222", false, now); err != nil { // upsert
		t.Fatal(err)
	}
	cs, err := s.ListConsents(ctx, sess.ID)
	if err != nil || len(cs) != 1 || cs[0].Consented {
		t.Fatalf("consents: %v %+v", err, cs)
	}
	if err := s.ResetConsents(ctx, sess.ID); err != nil {
		t.Fatal(err)
	}
	cs2, _ := s.ListConsents(ctx, sess.ID)
	if len(cs2) != 0 {
		t.Fatalf("consents should reset: %+v", cs2)
	}

	// end session cascades breakout rooms on delete
	if err := s.EndSession(ctx, sess.ID, now); err != nil {
		t.Fatal(err)
	}
	ended, err := s.GetSession(ctx, sess.ID)
	if err != nil || ended.EndedAt == nil {
		t.Fatalf("end: %v %+v", err, ended)
	}
}
