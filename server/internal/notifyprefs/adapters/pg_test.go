package adapters

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/whatsapp-v2/server/internal/notifyprefs"
	"github.com/whatsapp-v2/server/internal/notifyprefs/domain"
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

func TestIntegration_Prefs(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	s := NewStore(pool)
	u := seedUser(t, pool)

	// no row yet → ErrNotFound (service falls back to defaults)
	if _, err := s.GetPrefs(ctx, u); err != notifyprefs.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	p := domain.Prefs{Channels: domain.ChannelPush | domain.ChannelSMS, QuietStart: 1320, QuietEnd: 420, Sound: false, Vibrate: true}
	if err := s.UpsertPrefs(ctx, u, p); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetPrefs(ctx, u)
	if err != nil || got.Channels != p.Channels || got.QuietStart != 1320 || got.Sound {
		t.Fatalf("prefs round-trip: %v %+v", err, got)
	}
	// upsert again with quiet hours off → NULL columns → -1
	p.QuietStart, p.QuietEnd = -1, -1
	if err := s.UpsertPrefs(ctx, u, p); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetPrefs(ctx, u); got.QuietStart != -1 || got.QuietEnd != -1 {
		t.Fatalf("quiet-off round-trip: %+v", got)
	}
}

func TestIntegration_Snooze(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	s := NewStore(pool)
	u := seedUser(t, pool)
	conv := id.New()

	if got, _ := s.GetSnooze(ctx, u, conv); !got.IsZero() {
		t.Fatalf("unset snooze should be zero, got %v", got)
	}
	until := time.Now().Add(time.Hour).Truncate(time.Millisecond)
	if err := s.SetSnooze(ctx, u, conv, until); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetSnooze(ctx, u, conv); !got.Equal(until) {
		t.Fatalf("snooze round-trip: %v want %v", got, until)
	}
	// upsert a new time on the same conversation
	until2 := until.Add(time.Hour)
	if err := s.SetSnooze(ctx, u, conv, until2); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetSnooze(ctx, u, conv); !got.Equal(until2) {
		t.Fatalf("snooze upsert: %v want %v", got, until2)
	}
	if err := s.ClearSnooze(ctx, u, conv); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetSnooze(ctx, u, conv); !got.IsZero() {
		t.Fatalf("cleared snooze should be zero, got %v", got)
	}
}

func TestIntegration_Scheduled(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	s := NewStore(pool)
	u := seedUser(t, pool)
	conv := id.New()

	past := notifyprefs.ScheduledNotification{ID: id.New(), UserID: u, ConversationID: conv, Title: "Ping", DueAt: time.Now().Add(-time.Minute), CreatedAt: time.Now()}
	future := notifyprefs.ScheduledNotification{ID: id.New(), UserID: u, Title: "Later", DueAt: time.Now().Add(time.Hour), CreatedAt: time.Now()}
	for _, n := range []notifyprefs.ScheduledNotification{past, future} {
		if err := s.CreateScheduled(ctx, n); err != nil {
			t.Fatal(err)
		}
	}
	list, err := s.ListScheduled(ctx, u)
	if err != nil || len(list) != 2 {
		t.Fatalf("list: %v %d", err, len(list))
	}
	// due-scan surfaces only the past-due, not-yet-fired reminder
	due, err := s.DueBefore(ctx, time.Now(), 10)
	if err != nil || len(due) != 1 || due[0].ID != past.ID {
		t.Fatalf("due-before: %v %+v", err, due)
	}
	if err := s.MarkFired(ctx, past.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	if due, _ := s.DueBefore(ctx, time.Now(), 10); len(due) != 0 {
		t.Fatalf("fired reminder should not re-surface: %+v", due)
	}
	// the optional conversation_id round-trips (NULL for `future`)
	for _, n := range list {
		if n.ID == future.ID && n.ConversationID != "" {
			t.Fatalf("expected NULL conversation to scan as empty, got %q", n.ConversationID)
		}
		if n.ID == past.ID && n.ConversationID != conv {
			t.Fatalf("conversation_id round-trip: got %q want %q", n.ConversationID, conv)
		}
	}
	if err := s.DeleteScheduled(ctx, u, future.ID); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.ListScheduled(ctx, u); len(list) != 1 {
		t.Fatalf("after delete: %d", len(list))
	}
}
