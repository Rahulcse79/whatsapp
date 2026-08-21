package notifyprefs

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/notifyprefs/domain"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
)

type fakeStore struct {
	prefs     map[string]domain.Prefs
	snooze    map[string]time.Time // key: user|conv
	scheduled map[string]ScheduledNotification
}

func newFake() *fakeStore {
	return &fakeStore{prefs: map[string]domain.Prefs{}, snooze: map[string]time.Time{}, scheduled: map[string]ScheduledNotification{}}
}

func skey(u, c string) string { return u + "|" + c }

func (s *fakeStore) GetPrefs(_ context.Context, u string) (domain.Prefs, error) {
	p, ok := s.prefs[u]
	if !ok {
		return domain.Prefs{}, ErrNotFound
	}
	return p, nil
}
func (s *fakeStore) UpsertPrefs(_ context.Context, u string, p domain.Prefs) error {
	s.prefs[u] = p
	return nil
}
func (s *fakeStore) SetSnooze(_ context.Context, u, c string, until time.Time) error {
	s.snooze[skey(u, c)] = until
	return nil
}
func (s *fakeStore) ClearSnooze(_ context.Context, u, c string) error {
	delete(s.snooze, skey(u, c))
	return nil
}
func (s *fakeStore) GetSnooze(_ context.Context, u, c string) (time.Time, error) {
	return s.snooze[skey(u, c)], nil
}
func (s *fakeStore) CreateScheduled(_ context.Context, n ScheduledNotification) error {
	s.scheduled[n.ID] = n
	return nil
}
func (s *fakeStore) ListScheduled(_ context.Context, u string) ([]ScheduledNotification, error) {
	var out []ScheduledNotification
	for _, n := range s.scheduled {
		if n.UserID == u {
			out = append(out, n)
		}
	}
	return out, nil
}
func (s *fakeStore) DeleteScheduled(_ context.Context, u, id string) error {
	if n, ok := s.scheduled[id]; ok && n.UserID == u {
		delete(s.scheduled, id)
	}
	return nil
}
func (s *fakeStore) DueBefore(_ context.Context, cutoff time.Time, limit int) ([]ScheduledNotification, error) {
	var out []ScheduledNotification
	for _, n := range s.scheduled {
		if n.FiredAt == nil && !n.DueAt.After(cutoff) {
			out = append(out, n)
			if len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}
func (s *fakeStore) MarkFired(_ context.Context, id string, at time.Time) error {
	n := s.scheduled[id]
	n.FiredAt = &at
	s.scheduled[id] = n
	return nil
}

func who(u string) auth.Identity { return auth.Identity{UserID: u} }

func codeOf(t *testing.T, err error) string {
	t.Helper()
	var ae *httpx.APIError
	if !errors.As(err, &ae) {
		t.Fatalf("want *httpx.APIError, got %T: %v", err, err)
	}
	return ae.Code
}

func newSvc() (*Service, *fakeStore) {
	st := newFake()
	svc := NewService(st)
	n := 0
	svc.newID = func() string { n++; return fmt.Sprintf("sn%d", n) }
	svc.now = func() time.Time { return time.UnixMilli(1_000_000) }
	return svc, st
}

func TestGetPrefsDefaults(t *testing.T) {
	svc, _ := newSvc()
	p, err := svc.GetPrefs(context.Background(), who("alice"))
	if err != nil {
		t.Fatal(err)
	}
	if p.Channels != domain.DefaultMask || p.QuietStart != -1 {
		t.Fatalf("expected defaults, got %+v", p)
	}
}

func TestSetPrefsValidation(t *testing.T) {
	svc, _ := newSvc()
	bad := domain.Prefs{Channels: 1 << 6, QuietStart: -1, QuietEnd: -1}
	if err := svc.SetPrefs(context.Background(), who("alice"), bad); codeOf(t, err) != "VALIDATION_PREFS" {
		t.Fatal("bad channel bits should 400")
	}
	good := domain.Prefs{Channels: domain.ChannelPush | domain.ChannelEmail, QuietStart: 1320, QuietEnd: 420, Sound: true}
	if err := svc.SetPrefs(context.Background(), who("alice"), good); err != nil {
		t.Fatalf("valid prefs: %v", err)
	}
	got, _ := svc.GetPrefs(context.Background(), who("alice"))
	if !got.Has(domain.ChannelEmail) || got.QuietStart != 1320 {
		t.Fatalf("prefs not persisted: %+v", got)
	}
}

func TestSnoozeSetAndClear(t *testing.T) {
	svc, st := newSvc()
	future := time.UnixMilli(2_000_000)
	if err := svc.SetSnooze(context.Background(), who("alice"), "conv1", future); err != nil {
		t.Fatal(err)
	}
	if u, _ := st.GetSnooze(context.Background(), "alice", "conv1"); !u.Equal(future) {
		t.Fatalf("snooze not set: %v", u)
	}
	// a past time clears the snooze
	if err := svc.SetSnooze(context.Background(), who("alice"), "conv1", time.UnixMilli(1)); err != nil {
		t.Fatal(err)
	}
	if u, _ := st.GetSnooze(context.Background(), "alice", "conv1"); !u.IsZero() {
		t.Fatalf("snooze should be cleared, got %v", u)
	}
}

func TestScheduleAndFireDue(t *testing.T) {
	svc, _ := newSvc()
	// due in the past relative to a later FireDue clock is what we want, but
	// ScheduleNotification requires a future due time at creation.
	due := time.UnixMilli(1_500_000)
	v, err := svc.ScheduleNotification(context.Background(), who("alice"), "conv1", "Ping me", due)
	if err != nil || v.Fired {
		t.Fatalf("schedule: %v %+v", err, v)
	}
	// past due time is rejected
	if _, err := svc.ScheduleNotification(context.Background(), who("alice"), "", "late", time.UnixMilli(1)); codeOf(t, err) != "VALIDATION_DUE" {
		t.Fatal("past due should 400")
	}
	// advance the clock past the due time and fire
	svc.now = func() time.Time { return time.UnixMilli(2_000_000) }
	fired, err := svc.FireDue(context.Background(), 10)
	if err != nil || len(fired) != 1 || fired[0].FiredAt == nil {
		t.Fatalf("fire due: %v %+v", err, fired)
	}
	// a second scan fires nothing (already marked)
	again, _ := svc.FireDue(context.Background(), 10)
	if len(again) != 0 {
		t.Fatalf("already-fired reminder should not re-fire: %+v", again)
	}
}
