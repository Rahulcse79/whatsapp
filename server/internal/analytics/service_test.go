package analytics

import (
	"context"
	"testing"
	"time"

	"github.com/whatsapp-v2/server/internal/analytics/domain"
)

type fakeRollups struct {
	incr   map[string]int64
	set    map[string]int64
	purged int64
}

func newFakeRollups() *fakeRollups {
	return &fakeRollups{incr: map[string]int64{}, set: map[string]int64{}}
}

func (f *fakeRollups) IncrDaily(_ context.Context, day time.Time, metric string, delta int64) error {
	f.incr[domain.DayKey(day)+"|"+metric] += delta
	return nil
}
func (f *fakeRollups) SetDaily(_ context.Context, day time.Time, metric string, value int64) error {
	f.set[domain.DayKey(day)+"|"+metric] = value
	return nil
}
func (f *fakeRollups) Query(_ context.Context, _, _ time.Time) ([]DailyValue, error) { return nil, nil }
func (f *fakeRollups) PurgeOlderThan(_ context.Context, _ time.Time) (int64, error) {
	f.purged++
	return 3, nil
}

type fakeDistinct struct{ adds map[string][]string }

func newFakeDistinct() *fakeDistinct { return &fakeDistinct{adds: map[string][]string{}} }

func (f *fakeDistinct) Add(_ context.Context, bucket, userID string) error {
	f.adds[bucket] = append(f.adds[bucket], userID)
	return nil
}
func (f *fakeDistinct) Count(_ context.Context, buckets ...string) (int64, error) {
	seen := map[string]struct{}{}
	for _, b := range buckets {
		for _, u := range f.adds[b] {
			seen[u] = struct{}{}
		}
	}
	return int64(len(seen)), nil
}

func fixedNow() time.Time { return time.Date(2026, 8, 13, 10, 0, 0, 0, time.UTC) }

func TestIngest_RoutesCountersAndDistinct(t *testing.T) {
	r, d := newFakeRollups(), newFakeDistinct()
	svc := NewService(r, d, nil) // nil metrics: setters are nil-safe
	svc.now = fixedNow
	ctx := context.Background()

	_ = svc.Ingest(ctx, Event{Kind: domain.KindSignup})                      // counter, delta defaults to 1
	_ = svc.Ingest(ctx, Event{Kind: domain.KindMessageRelayed, Delta: 5})    // counter, explicit delta
	_ = svc.Ingest(ctx, Event{Kind: domain.KindFlagExposure, Label: "dark"}) // labelled counter
	_ = svc.Ingest(ctx, Event{Kind: domain.KindActiveUser, UserID: "u1"})    // distinct sketch
	_ = svc.Ingest(ctx, Event{Kind: domain.KindActiveUser, UserID: ""})      // dropped — no user id
	_ = svc.Ingest(ctx, Event{Kind: domain.EventKind("bogus")})              // dropped — unknown kind

	if r.incr["2026-08-13|signups"] != 1 {
		t.Errorf("signups = %d, want 1", r.incr["2026-08-13|signups"])
	}
	if r.incr["2026-08-13|messages_relayed"] != 5 {
		t.Errorf("messages_relayed = %d, want 5", r.incr["2026-08-13|messages_relayed"])
	}
	if r.incr["2026-08-13|flag_exposure:dark"] != 1 {
		t.Errorf("flag_exposure:dark = %d, want 1", r.incr["2026-08-13|flag_exposure:dark"])
	}
	if got := d.adds["active_user:2026-08-13"]; len(got) != 1 || got[0] != "u1" {
		t.Errorf("distinct adds = %v, want [u1] (empty user dropped)", got)
	}
}

func TestRollupDistinct_ComputesDAUAndMAU(t *testing.T) {
	r, d := newFakeRollups(), newFakeDistinct()
	svc := NewService(r, d, nil)
	ctx := context.Background()
	day := domain.Day(fixedNow())

	_ = d.Add(ctx, "active_user:2026-08-13", "u1")
	_ = d.Add(ctx, "active_user:2026-08-13", "u2")
	_ = d.Add(ctx, "active_user:2026-08-08", "u3") // 5 days back — inside the 30-day MAU window

	if err := svc.RollupDistinct(ctx, day); err != nil {
		t.Fatal(err)
	}
	if r.set["2026-08-13|dau"] != 2 {
		t.Errorf("dau = %d, want 2 (today's distinct)", r.set["2026-08-13|dau"])
	}
	if r.set["2026-08-13|mau"] != 3 {
		t.Errorf("mau = %d, want 3 (30-day union)", r.set["2026-08-13|mau"])
	}
}

func TestPurge(t *testing.T) {
	r := newFakeRollups()
	svc := NewService(r, newFakeDistinct(), nil)
	n, err := svc.Purge(context.Background())
	if err != nil || n != 3 {
		t.Fatalf("Purge = (%d, %v), want (3, nil)", n, err)
	}
}
