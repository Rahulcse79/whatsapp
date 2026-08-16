package abuse

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/whatsapp-v2/server/internal/abuse/domain"
	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
	"github.com/whatsapp-v2/server/internal/platform/ratelimit"
)

type fakeStore struct {
	users map[string]bool
	filed []Report
}

func (s *fakeStore) FileReport(_ context.Context, r Report) error {
	s.filed = append(s.filed, r)
	return nil
}
func (s *fakeStore) UserExists(_ context.Context, userID string) (bool, error) {
	return s.users[userID], nil
}

type fakeLimiter struct{ deny bool }

func (l *fakeLimiter) Allow(_ context.Context, _ string, _ ratelimit.Params) (ratelimit.Result, error) {
	return ratelimit.Result{Allowed: !l.deny, RetryAfter: time.Minute}, nil
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

func newSvc(deny bool) (*Service, *fakeStore) {
	store := &fakeStore{users: map[string]bool{"bob": true}}
	svc := NewService(store, &fakeLimiter{deny: deny})
	n := 0
	svc.newID = func() string { n++; return "rep" }
	svc.now = func() time.Time { return time.UnixMilli(1_000_000) }
	return svc, store
}

func TestReportFilesIntoQueue(t *testing.T) {
	svc, store := newSvc(false)
	res, err := svc.Report(context.Background(), who("alice"), "bob", domain.ReasonScam, "phishing link", nil)
	if err != nil || res.ReportID == "" {
		t.Fatalf("report: %v %+v", err, res)
	}
	if len(store.filed) != 1 || store.filed[0].TargetUserID != "bob" || store.filed[0].Reason != domain.ReasonScam {
		t.Fatalf("filed report wrong: %+v", store.filed)
	}
}

func TestReportRejectsSelfAndUnknownAndBadReason(t *testing.T) {
	svc, _ := newSvc(false)
	if _, err := svc.Report(context.Background(), who("alice"), "alice", domain.ReasonSpam, "", nil); codeOf(t, err) != "VALIDATION_TARGET" {
		t.Fatal("self-report should fail")
	}
	if _, err := svc.Report(context.Background(), who("alice"), "ghost", domain.ReasonSpam, "", nil); codeOf(t, err) != "USER_NOT_FOUND" {
		t.Fatal("unknown target should 404")
	}
	if _, err := svc.Report(context.Background(), who("alice"), "bob", domain.Reason(99), "", nil); codeOf(t, err) != "VALIDATION_REPORT" {
		t.Fatal("bad reason should validate")
	}
}

func TestReportRateLimited(t *testing.T) {
	svc, _ := newSvc(true)
	if _, err := svc.Report(context.Background(), who("alice"), "bob", domain.ReasonSpam, "", nil); codeOf(t, err) != "RATE_LIMITED" {
		t.Fatal("rate-limited report should 429")
	}
}

func TestReportDisclosureOnlyWhenProvided(t *testing.T) {
	svc, store := newSvc(false)
	_, _ = svc.Report(context.Background(), who("alice"), "bob", domain.ReasonHarassment, "", []byte("sealed"))
	if len(store.filed[0].DisclosedCiphertext) == 0 {
		t.Fatal("disclosed ciphertext should be attached when provided")
	}
}
