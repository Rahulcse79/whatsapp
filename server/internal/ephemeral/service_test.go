package ephemeral

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
)

type fakeStore struct {
	members map[string]map[string]bool // convID → userID → member
	timers  map[string]int
	purged  int
	purgeAt time.Time
}

func newFake() *fakeStore {
	return &fakeStore{members: map[string]map[string]bool{}, timers: map[string]int{}}
}
func (s *fakeStore) IsMember(_ context.Context, convID, userID string) (bool, error) {
	return s.members[convID][userID], nil
}
func (s *fakeStore) GetTimer(_ context.Context, convID string) (int, error) {
	return s.timers[convID], nil
}
func (s *fakeStore) SetTimer(_ context.Context, convID string, ttl int, _ string, _ time.Time) error {
	s.timers[convID] = ttl
	return nil
}
func (s *fakeStore) PurgeExpired(_ context.Context, now time.Time) (int, error) {
	s.purgeAt = now
	return s.purged, nil
}

func codeOf(t *testing.T, err error) string {
	t.Helper()
	var ae *httpx.APIError
	if !errors.As(err, &ae) {
		t.Fatalf("want *httpx.APIError, got %T: %v", err, err)
	}
	return ae.Code
}

func who(u string) auth.Identity { return auth.Identity{UserID: u} }

func TestSetAndGetTimer(t *testing.T) {
	st := newFake()
	st.members["c1"] = map[string]bool{"alice": true}
	svc := NewService(st)

	if err := svc.SetTimer(context.Background(), who("alice"), "c1", 86_400); err != nil {
		t.Fatalf("set: %v", err)
	}
	got, err := svc.GetTimer(context.Background(), who("alice"), "c1")
	if err != nil || got != 86_400 {
		t.Fatalf("get: %v %d", err, got)
	}
}

func TestNonMemberIs404(t *testing.T) {
	st := newFake()
	st.members["c1"] = map[string]bool{"alice": true}
	svc := NewService(st)

	if _, err := svc.GetTimer(context.Background(), who("mallory"), "c1"); codeOf(t, err) != "CONVERSATION_NOT_FOUND" {
		t.Fatal("non-member get should 404")
	}
	if err := svc.SetTimer(context.Background(), who("mallory"), "c1", 3600); codeOf(t, err) != "CONVERSATION_NOT_FOUND" {
		t.Fatal("non-member set should 404")
	}
}

func TestSetTimerValidates(t *testing.T) {
	st := newFake()
	st.members["c1"] = map[string]bool{"alice": true}
	svc := NewService(st)
	if err := svc.SetTimer(context.Background(), who("alice"), "c1", -5); codeOf(t, err) != "VALIDATION_TTL" {
		t.Fatal("negative ttl should validate")
	}
	if err := svc.SetTimer(context.Background(), who("alice"), "c1", 1<<40); codeOf(t, err) != "VALIDATION_TTL" {
		t.Fatal("over-max ttl should validate")
	}
}

func TestPurgeExpired(t *testing.T) {
	st := newFake()
	st.purged = 7
	svc := NewService(st)
	svc.now = func() time.Time { return time.UnixMilli(5_000_000) }
	n, err := svc.PurgeExpired(context.Background())
	if err != nil || n != 7 || !st.purgeAt.Equal(time.UnixMilli(5_000_000)) {
		t.Fatalf("purge = (%d,%v) at %v", n, err, st.purgeAt)
	}
}
