package webinar

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
	"github.com/whatsapp-v2/server/internal/webinar/domain"
)

// ── fakes ────────────────────────────────────────────────────────────────

type fakeStore struct {
	byID  map[string]Webinar
	parts map[string]map[string]Participant // webinarID → userID → participant
	qs    map[string]map[string]Question    // webinarID → qid → question
	votes map[string]map[string]bool        // qid → voterID set
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		byID:  map[string]Webinar{},
		parts: map[string]map[string]Participant{},
		qs:    map[string]map[string]Question{},
		votes: map[string]map[string]bool{},
	}
}
func (s *fakeStore) Create(_ context.Context, w Webinar) error { s.byID[w.ID] = w; return nil }
func (s *fakeStore) Get(_ context.Context, id string) (Webinar, error) {
	w, ok := s.byID[id]
	if !ok {
		return Webinar{}, ErrNotFound
	}
	return w, nil
}
func (s *fakeStore) End(_ context.Context, id string, at time.Time) error {
	w := s.byID[id]
	w.EndedAt = &at
	s.byID[id] = w
	return nil
}
func (s *fakeStore) UpsertParticipant(_ context.Context, wid string, p Participant) error {
	if s.parts[wid] == nil {
		s.parts[wid] = map[string]Participant{}
	}
	if _, ok := s.parts[wid][p.UserID]; !ok {
		s.parts[wid][p.UserID] = p
	}
	return nil
}
func (s *fakeStore) GetParticipant(_ context.Context, wid, uid string) (Participant, error) {
	p, ok := s.parts[wid][uid]
	if !ok {
		return Participant{}, ErrNotFound
	}
	return p, nil
}
func (s *fakeStore) SetStatus(_ context.Context, wid, uid string, status domain.Status, leftAt *time.Time) error {
	p := s.parts[wid][uid]
	p.Status = status
	p.LeftAt = leftAt
	s.parts[wid][uid] = p
	return nil
}
func (s *fakeStore) SetRole(_ context.Context, wid, uid string, role domain.Role) error {
	p := s.parts[wid][uid]
	p.Role = role
	s.parts[wid][uid] = p
	return nil
}
func (s *fakeStore) SetHand(_ context.Context, wid, uid string, raised bool) error {
	p := s.parts[wid][uid]
	p.HandRaised = raised
	s.parts[wid][uid] = p
	return nil
}
func (s *fakeStore) ListParticipants(_ context.Context, wid string) ([]Participant, error) {
	var out []Participant
	for _, p := range s.parts[wid] {
		out = append(out, p)
	}
	return out, nil
}
func (s *fakeStore) CreateQuestion(_ context.Context, wid string, q Question) error {
	if s.qs[wid] == nil {
		s.qs[wid] = map[string]Question{}
	}
	s.qs[wid][q.ID] = q
	return nil
}
func (s *fakeStore) ListQuestions(_ context.Context, wid string) ([]Question, error) {
	var out []Question
	for _, q := range s.qs[wid] {
		q.Upvotes = len(s.votes[q.ID])
		out = append(out, q)
	}
	return out, nil
}
func (s *fakeStore) UpvoteQuestion(_ context.Context, _, qid, voter string) error {
	if s.votes[qid] == nil {
		s.votes[qid] = map[string]bool{}
	}
	s.votes[qid][voter] = true
	return nil
}
func (s *fakeStore) AnswerQuestion(_ context.Context, wid, qid string) error {
	q := s.qs[wid][qid]
	q.Answered = true
	s.qs[wid][qid] = q
	return nil
}

type fakeMinter struct{ calls []bool }

func (m *fakeMinter) MintJoin(_, _ string, canPublish bool) (string, error) {
	m.calls = append(m.calls, canPublish)
	if canPublish {
		return "token-publish", nil
	}
	return "token-subscribe", nil
}

// ── helpers ──────────────────────────────────────────────────────────────

func codeOf(t *testing.T, err error) string {
	t.Helper()
	var ae *httpx.APIError
	if !errors.As(err, &ae) {
		t.Fatalf("want *httpx.APIError, got %T: %v", err, err)
	}
	return ae.Code
}

func newSvc() (*Service, *fakeStore, *fakeMinter) {
	store := newFakeStore()
	m := &fakeMinter{}
	svc := NewService(store, m)
	n := 0
	svc.newID = func() string { n++; return fmt.Sprintf("id%d", n) }
	svc.now = func() time.Time { return time.UnixMilli(1_000_000) }
	return svc, store, m
}

func who(u string) auth.Identity { return auth.Identity{UserID: u} }

// ── tests ──────────────────────────────────────────────────────────────────

func TestCreateHostGetsPublishToken(t *testing.T) {
	svc, _, m := newSvc()
	res, err := svc.Create(context.Background(), who("host"), "Launch")
	if err != nil || res.JoinToken != "token-publish" {
		t.Fatalf("create: %v %+v", err, res)
	}
	if m.calls[0] != true {
		t.Fatal("host token must be publish")
	}
	if _, err := svc.Create(context.Background(), who("host"), ""); codeOf(t, err) != "VALIDATION_TITLE" {
		t.Fatal("blank title rejected")
	}
}

func TestWaitingRoomAndAdmission(t *testing.T) {
	svc, _, _ := newSvc()
	w, _ := svc.Create(context.Background(), who("host"), "W")

	// attendee joins → waiting, no token
	me, err := svc.Join(context.Background(), who("a"), w.WebinarID)
	if err != nil || me.Status != "waiting" || me.JoinToken != "" {
		t.Fatalf("join → waiting no token: %v %+v", err, me)
	}
	// host admits → attendee now has a subscribe-only token
	if err := svc.Admit(context.Background(), who("host"), w.WebinarID, "a"); err != nil {
		t.Fatalf("admit: %v", err)
	}
	me, _ = svc.Me(context.Background(), who("a"), w.WebinarID)
	if me.Status != "admitted" || me.JoinToken != "token-subscribe" || me.CanPublish {
		t.Fatalf("admitted attendee should be subscribe-only: %+v", me)
	}
	// non-host can't admit
	if err := svc.Admit(context.Background(), who("a"), w.WebinarID, "a"); codeOf(t, err) != "WEBINAR_NOT_FOUND" {
		t.Fatal("non-host admit should 404")
	}
}

func TestRaiseHandAndPromote(t *testing.T) {
	svc, _, _ := newSvc()
	w, _ := svc.Create(context.Background(), who("host"), "W")
	_, _ = svc.Join(context.Background(), who("a"), w.WebinarID)
	_ = svc.Admit(context.Background(), who("host"), w.WebinarID, "a")

	if err := svc.SetHand(context.Background(), who("a"), w.WebinarID, true); err != nil {
		t.Fatalf("raise hand: %v", err)
	}
	// promote → speaker → now publishes, hand cleared
	if err := svc.SetRole(context.Background(), who("host"), w.WebinarID, "a", "speaker"); err != nil {
		t.Fatalf("promote: %v", err)
	}
	me, _ := svc.Me(context.Background(), who("a"), w.WebinarID)
	if me.Role != "speaker" || !me.CanPublish || me.JoinToken != "token-publish" || me.HandRaised {
		t.Fatalf("promoted speaker should publish + hand cleared: %+v", me)
	}
}

func TestRosterAndAttendance(t *testing.T) {
	svc, _, _ := newSvc()
	w, _ := svc.Create(context.Background(), who("host"), "W")
	_, _ = svc.Join(context.Background(), who("a"), w.WebinarID)
	_ = svc.Leave(context.Background(), who("a"), w.WebinarID)

	roster, err := svc.Roster(context.Background(), who("host"), w.WebinarID)
	if err != nil || len(roster) != 2 {
		t.Fatalf("roster: %v (%d)", err, len(roster))
	}
	var left *RosterEntry
	for i := range roster {
		if roster[i].UserID == "a" {
			left = &roster[i]
		}
	}
	if left == nil || left.Status != "left" || left.LeftAtMS == 0 {
		t.Fatalf("attendance should record leave: %+v", left)
	}
	// attendee can't read the roster
	if _, err := svc.Roster(context.Background(), who("a"), w.WebinarID); codeOf(t, err) != "WEBINAR_NOT_FOUND" {
		t.Fatal("non-host roster should 404")
	}
}

func TestQA(t *testing.T) {
	svc, _, _ := newSvc()
	w, _ := svc.Create(context.Background(), who("host"), "W")
	_, _ = svc.Join(context.Background(), who("a"), w.WebinarID)
	_ = svc.Admit(context.Background(), who("host"), w.WebinarID, "a")

	q, err := svc.AskQuestion(context.Background(), who("a"), w.WebinarID, "Why?")
	if err != nil {
		t.Fatalf("ask: %v", err)
	}
	if err := svc.UpvoteQuestion(context.Background(), who("host"), w.WebinarID, q.ID); err != nil {
		t.Fatalf("upvote: %v", err)
	}
	if err := svc.AnswerQuestion(context.Background(), who("host"), w.WebinarID, q.ID); err != nil {
		t.Fatalf("answer: %v", err)
	}
	qs, _ := svc.Questions(context.Background(), who("a"), w.WebinarID)
	if len(qs) != 1 || qs[0].Upvotes != 1 || !qs[0].Answered {
		t.Fatalf("Q&A state: %+v", qs)
	}
	// a non-participant can't ask
	if _, err := svc.AskQuestion(context.Background(), who("stranger"), w.WebinarID, "hi"); codeOf(t, err) != "WEBINAR_NOT_FOUND" {
		t.Fatal("non-participant ask should 404")
	}
}
