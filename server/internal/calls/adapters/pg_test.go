package adapters

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/whatsapp-v2/server/internal/calls"
	"github.com/whatsapp-v2/server/internal/calls/domain"
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

// call_records has no FK on initiator/participants (records outlive deleted
// accounts as tombstoned ids), so history rows need no seeded users.
func TestIntegration_HistoryRoundTripAndPurge(t *testing.T) {
	pool := testPool(t)
	h := NewHistory(pool)
	ctx := context.Background()

	user := id.New()
	now := time.Now().UTC()
	old := now.Add(-100 * 24 * time.Hour) // beyond 90-day retention
	recent := now.Add(-1 * time.Hour)
	oldID, recentID := id.New(), id.New()

	mustUpsert(t, h, calls.CallRecord{
		ID: oldID, RoomID: "room-old", Kind: domain.KindVoice, Initiator: user,
		Participants: []string{user}, StartedAt: &old, Outcome: calls.OutcomeCompleted,
	})
	mustUpsert(t, h, calls.CallRecord{
		ID: recentID, RoomID: "room-new", Kind: domain.KindVideo, Initiator: user,
		Participants: []string{user}, StartedAt: &recent, Outcome: calls.OutcomeMissed,
	})

	// List returns both (newest first) with kind round-tripped across the DB shift.
	recs, _, err := h.List(ctx, user, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 {
		t.Fatalf("list = %d, want 2", len(recs))
	}
	byID := map[string]calls.CallRecord{}
	for _, r := range recs {
		byID[r.ID] = r
	}
	if byID[oldID].Kind != domain.KindVoice || byID[recentID].Kind != domain.KindVideo {
		t.Errorf("kind did not round-trip: %+v", byID)
	}
	if byID[recentID].Outcome != calls.OutcomeMissed {
		t.Errorf("outcome did not round-trip: %+v", byID[recentID])
	}

	// Purge beyond the retention window removes only the old record.
	n, err := h.PurgeOlderThan(ctx, now.Add(-domain.HistoryRetention))
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("purged %d, want 1", n)
	}
	recs, _, err = h.List(ctx, user, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 || recs[0].ID != recentID {
		t.Fatalf("after purge = %+v, want only the recent record", recs)
	}
}

func mustUpsert(t *testing.T, h *History, rec calls.CallRecord) {
	t.Helper()
	if err := h.Upsert(context.Background(), rec); err != nil {
		t.Fatal(err)
	}
}
