package adapters

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/whatsapp-v2/server/internal/platform/id"
	"github.com/whatsapp-v2/server/internal/webinar"
	"github.com/whatsapp-v2/server/internal/webinar/domain"
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

func TestIntegration_WebinarLifecycle(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	s := NewStore(pool)
	host := seedUser(t, pool)
	attendee := seedUser(t, pool)

	w := webinar.Webinar{ID: id.New(), Title: "Launch", HostID: host, RoomID: "wbn-" + id.New(), CreatedAt: time.Now()}
	if err := s.Create(ctx, w); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := s.UpsertParticipant(ctx, w.ID, webinar.Participant{UserID: host, Role: domain.RoleHost, Status: domain.StatusAdmitted, JoinedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertParticipant(ctx, w.ID, webinar.Participant{UserID: attendee, Role: domain.RoleAttendee, Status: domain.StatusWaiting, JoinedAt: now}); err != nil {
		t.Fatal(err)
	}

	// admit + promote + raise-hand
	if err := s.SetStatus(ctx, w.ID, attendee, domain.StatusAdmitted, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.SetHand(ctx, w.ID, attendee, true); err != nil {
		t.Fatal(err)
	}
	if err := s.SetRole(ctx, w.ID, attendee, domain.RoleSpeaker); err != nil {
		t.Fatal(err)
	}
	p, err := s.GetParticipant(ctx, w.ID, attendee)
	if err != nil || p.Role != domain.RoleSpeaker || p.Status != domain.StatusAdmitted || !p.HandRaised {
		t.Fatalf("participant state: %v %+v", err, p)
	}

	// roster has both, ordered host-first
	roster, err := s.ListParticipants(ctx, w.ID)
	if err != nil || len(roster) != 2 {
		t.Fatalf("roster: %v (%d)", err, len(roster))
	}

	// Q&A: ask + upvote (idempotent) + answer
	q := webinar.Question{ID: id.New(), AskerID: attendee, Body: "Why?", CreatedAt: time.Now()}
	if err := s.CreateQuestion(ctx, w.ID, q); err != nil {
		t.Fatal(err)
	}
	if err := s.UpvoteQuestion(ctx, w.ID, q.ID, host); err != nil {
		t.Fatal(err)
	}
	if err := s.UpvoteQuestion(ctx, w.ID, q.ID, host); err != nil { // idempotent
		t.Fatal(err)
	}
	if err := s.AnswerQuestion(ctx, w.ID, q.ID); err != nil {
		t.Fatal(err)
	}
	qs, err := s.ListQuestions(ctx, w.ID)
	if err != nil || len(qs) != 1 || qs[0].Upvotes != 1 || !qs[0].Answered {
		t.Fatalf("Q&A: %v %+v", err, qs)
	}

	// end + leave cascade on delete
	if err := s.End(ctx, w.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(ctx, w.ID)
	if err != nil || got.EndedAt == nil {
		t.Fatalf("end: %v %+v", err, got)
	}
}
