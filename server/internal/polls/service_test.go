package polls

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
)

// ── fake store ─────────────────────────────────────────────────────────────

type fakeStore struct {
	byID  map[string]Poll
	votes map[string]map[string][]int // pollID → voterID → indices
}

func newFakeStore() *fakeStore {
	return &fakeStore{byID: map[string]Poll{}, votes: map[string]map[string][]int{}}
}
func (s *fakeStore) Create(_ context.Context, p Poll) error { s.byID[p.ID] = p; return nil }
func (s *fakeStore) Get(_ context.Context, id string) (Poll, error) {
	p, ok := s.byID[id]
	if !ok {
		return Poll{}, ErrNotFound
	}
	return p, nil
}
func (s *fakeStore) SetClosed(_ context.Context, id string) error {
	p := s.byID[id]
	p.Closed = true
	s.byID[id] = p
	return nil
}
func (s *fakeStore) ReplaceVotes(_ context.Context, pollID, voterID string, indices []int) error {
	if s.votes[pollID] == nil {
		s.votes[pollID] = map[string][]int{}
	}
	s.votes[pollID][voterID] = indices
	return nil
}
func (s *fakeStore) Tally(_ context.Context, pollID string, optionCount int, voterID string) ([]int, int, []int, error) {
	counts := make([]int, optionCount)
	total := 0
	for _, idxs := range s.votes[pollID] {
		if len(idxs) > 0 {
			total++
		}
		for _, i := range idxs {
			if i >= 0 && i < optionCount {
				counts[i]++
			}
		}
	}
	return counts, total, s.votes[pollID][voterID], nil
}

// ── helpers ────────────────────────────────────────────────────────────────

func codeOf(t *testing.T, err error) string {
	t.Helper()
	var ae *httpx.APIError
	if !errors.As(err, &ae) {
		t.Fatalf("want *httpx.APIError, got %T: %v", err, err)
	}
	return ae.Code
}

func newSvc(store Store) *Service {
	s := NewService(store)
	n := 0
	s.newID = func() string { n++; return "poll" }
	s.now = func() time.Time { return time.UnixMilli(1_000_000) }
	return s
}

func who(u string) auth.Identity { return auth.Identity{UserID: u} }

// ── tests ──────────────────────────────────────────────────────────────────

func TestCreateValidatesOptionCount(t *testing.T) {
	svc := newSvc(newFakeStore())
	if _, err := svc.Create(context.Background(), who("u1"), "c1", 1, false, 0); codeOf(t, err) != "VALIDATION_OPTIONS" {
		t.Fatalf("want VALIDATION_OPTIONS")
	}
	res, err := svc.Create(context.Background(), who("u1"), "c1", 3, false, 0)
	if err != nil || res.PollID == "" {
		t.Fatalf("create: %v %+v", err, res)
	}
}

func TestVoteLifecycle(t *testing.T) {
	store := newFakeStore()
	svc := newSvc(store)
	res, _ := svc.Create(context.Background(), who("u1"), "c1", 3, false, 0)

	// single-choice rejects two picks
	if err := svc.Vote(context.Background(), who("u2"), res.PollID, []int{0, 1}); codeOf(t, err) != "VALIDATION_VOTE" {
		t.Fatalf("want VALIDATION_VOTE for two picks")
	}
	// out-of-range
	if err := svc.Vote(context.Background(), who("u2"), res.PollID, []int{9}); codeOf(t, err) != "VALIDATION_VOTE" {
		t.Fatalf("want VALIDATION_VOTE for range")
	}
	// valid, then re-vote replaces
	if err := svc.Vote(context.Background(), who("u2"), res.PollID, []int{0}); err != nil {
		t.Fatalf("vote: %v", err)
	}
	if err := svc.Vote(context.Background(), who("u2"), res.PollID, []int{2}); err != nil {
		t.Fatalf("re-vote: %v", err)
	}
	out, _ := svc.Results(context.Background(), who("u2"), res.PollID)
	if out.TotalVoters != 1 || out.Tallies[2] != 1 || out.Tallies[0] != 0 {
		t.Fatalf("re-vote should replace: %+v", out)
	}
	if len(out.MyVotes) != 1 || out.MyVotes[0] != 2 {
		t.Fatalf("my_votes: %+v", out.MyVotes)
	}
}

func TestVoteRejectedWhenClosed(t *testing.T) {
	store := newFakeStore()
	svc := newSvc(store)
	res, _ := svc.Create(context.Background(), who("u1"), "c1", 2, false, 0)

	// non-creator cannot close
	if err := svc.Close(context.Background(), who("u2"), res.PollID); codeOf(t, err) != "POLL_NOT_FOUND" {
		t.Fatalf("non-creator close should 404")
	}
	if err := svc.Close(context.Background(), who("u1"), res.PollID); err != nil {
		t.Fatalf("creator close: %v", err)
	}
	if err := svc.Vote(context.Background(), who("u2"), res.PollID, []int{0}); codeOf(t, err) != "POLL_CLOSED" {
		t.Fatalf("want POLL_CLOSED")
	}
}

func TestMultiChoiceAndDeadline(t *testing.T) {
	store := newFakeStore()
	svc := newSvc(store)
	// closes in the past → isClosed via deadline
	res, _ := svc.Create(context.Background(), who("u1"), "c1", 4, true, 500_000)
	if err := svc.Vote(context.Background(), who("u2"), res.PollID, []int{0, 2}); codeOf(t, err) != "POLL_CLOSED" {
		t.Fatalf("past deadline should close voting")
	}

	// a live multi poll accepts multiple picks
	res2, _ := svc.Create(context.Background(), who("u1"), "c1", 4, true, 0)
	if err := svc.Vote(context.Background(), who("u2"), res2.PollID, []int{0, 2}); err != nil {
		t.Fatalf("multi vote: %v", err)
	}
	out, _ := svc.Results(context.Background(), who("u3"), res2.PollID)
	if out.Tallies[0] != 1 || out.Tallies[2] != 1 || !out.Multi {
		t.Fatalf("multi tally: %+v", out)
	}
}
