package breakout

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/breakout/domain"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
)

// ── fakes ────────────────────────────────────────────────────────────────────

type fakeStore struct {
	sessions map[string]Session
	rooms    map[string][]Room             // sessionID → rooms
	assign   map[string]map[string]*string // sessionID → userID → roomID (nil = main)
	consents map[string]map[string]Consent // sessionID → userID → consent
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		sessions: map[string]Session{},
		rooms:    map[string][]Room{},
		assign:   map[string]map[string]*string{},
		consents: map[string]map[string]Consent{},
	}
}

func (s *fakeStore) CreateSession(_ context.Context, ss Session) error {
	s.sessions[ss.ID] = ss
	return nil
}
func (s *fakeStore) GetSession(_ context.Context, id string) (Session, error) {
	ss, ok := s.sessions[id]
	if !ok {
		return Session{}, ErrNotFound
	}
	return ss, nil
}
func (s *fakeStore) EndSession(_ context.Context, id string, at time.Time) error {
	ss := s.sessions[id]
	ss.EndedAt = &at
	s.sessions[id] = ss
	return nil
}
func (s *fakeStore) SetEgress(_ context.Context, id string, state domain.EgressState, kind domain.EgressKind, url, ref string) error {
	ss := s.sessions[id]
	ss.EgressState, ss.EgressKind, ss.EgressURL, ss.EgressRef = state, kind, url, ref
	s.sessions[id] = ss
	return nil
}
func (s *fakeStore) SetRecording(_ context.Context, id string, state domain.RecordingState) error {
	ss := s.sessions[id]
	ss.Recording = state
	s.sessions[id] = ss
	return nil
}
func (s *fakeStore) CreateRoom(_ context.Context, r Room) error {
	s.rooms[r.SessionID] = append(s.rooms[r.SessionID], r)
	return nil
}
func (s *fakeStore) ListRooms(_ context.Context, sessionID string) ([]Room, error) {
	var out []Room
	for _, r := range s.rooms[sessionID] {
		if r.ClosedAt == nil {
			out = append(out, r)
		}
	}
	return out, nil
}
func (s *fakeStore) CloseRooms(_ context.Context, sessionID string, at time.Time) error {
	rs := s.rooms[sessionID]
	for i := range rs {
		if rs[i].ClosedAt == nil {
			rs[i].ClosedAt = &at
		}
	}
	s.rooms[sessionID] = rs
	return nil
}
func (s *fakeStore) CountByRoom(_ context.Context, sessionID string) (map[string]int, error) {
	out := map[string]int{}
	for _, rid := range s.assign[sessionID] {
		if rid != nil {
			out[*rid]++
		}
	}
	return out, nil
}
func (s *fakeStore) SetAssignment(_ context.Context, sessionID, userID string, roomID *string, _ time.Time) error {
	if s.assign[sessionID] == nil {
		s.assign[sessionID] = map[string]*string{}
	}
	s.assign[sessionID][userID] = roomID
	return nil
}
func (s *fakeStore) GetAssignment(_ context.Context, sessionID, userID string) (Assignment, error) {
	m := s.assign[sessionID]
	if m == nil {
		return Assignment{}, ErrNotFound
	}
	rid, ok := m[userID]
	if !ok {
		return Assignment{}, ErrNotFound
	}
	return Assignment{UserID: userID, RoomID: rid}, nil
}
func (s *fakeStore) ClearAssignments(_ context.Context, sessionID string) error {
	delete(s.assign, sessionID)
	return nil
}
func (s *fakeStore) ResetConsents(_ context.Context, sessionID string) error {
	delete(s.consents, sessionID)
	return nil
}
func (s *fakeStore) SetConsent(_ context.Context, sessionID, userID string, consented bool, at time.Time) error {
	if s.consents[sessionID] == nil {
		s.consents[sessionID] = map[string]Consent{}
	}
	s.consents[sessionID][userID] = Consent{UserID: userID, Decided: true, Consented: consented, DecidedAt: at}
	return nil
}
func (s *fakeStore) ListConsents(_ context.Context, sessionID string) ([]Consent, error) {
	var out []Consent
	for _, c := range s.consents[sessionID] {
		out = append(out, c)
	}
	return out, nil
}

type fakeMinter struct{ calls int }

func (m *fakeMinter) MintJoin(room, _ string, _ bool) (string, error) {
	m.calls++
	return "token:" + room, nil
}

type fakeEgress struct {
	started []string
	stopped []string
	fail    bool
}

func (e *fakeEgress) Start(_ context.Context, room string, _ domain.EgressKind, _ string) (string, error) {
	if e.fail {
		return "", errors.New("boom")
	}
	e.started = append(e.started, room)
	return "egr-" + room, nil
}
func (e *fakeEgress) Stop(_ context.Context, ref string) error {
	e.stopped = append(e.stopped, ref)
	return nil
}

// ── helpers ──────────────────────────────────────────────────────────────────

func codeOf(t *testing.T, err error) string {
	t.Helper()
	var ae *httpx.APIError
	if !errors.As(err, &ae) {
		t.Fatalf("want *httpx.APIError, got %T: %v", err, err)
	}
	return ae.Code
}

func newSvc() (*Service, *fakeStore, *fakeEgress) {
	store := newFakeStore()
	egr := &fakeEgress{}
	svc := NewService(store, &fakeMinter{}, egr)
	n := 0
	svc.newID = func() string { n++; return fmt.Sprintf("id%d", n) }
	svc.now = func() time.Time { return time.UnixMilli(1_000_000) }
	return svc, store, egr
}

func who(u string) auth.Identity { return auth.Identity{UserID: u} }

// ── tests ────────────────────────────────────────────────────────────────────

func TestOpenAndStatus(t *testing.T) {
	svc, _, _ := newSvc()
	res, err := svc.Open(context.Background(), who("host"), "call-room-1")
	if err != nil || res.MainRoom != "call-room-1" {
		t.Fatalf("open: %v %+v", err, res)
	}
	st, err := svc.Status(context.Background(), who("host"), res.SessionID)
	if err != nil || st.Egress.State != "off" || st.Recording.State != "off" || st.MyRoom != nil {
		t.Fatalf("status: %v %+v", err, st)
	}
}

func TestBreakoutAssignAndMe(t *testing.T) {
	svc, _, _ := newSvc()
	sess, _ := svc.Open(context.Background(), who("host"), "main")

	// non-host can't create rooms
	if _, err := svc.CreateRooms(context.Background(), who("mallory"), sess.SessionID, []string{"A"}); codeOf(t, err) != "LIVE_NOT_FOUND" {
		t.Fatal("non-host create should 404")
	}
	rooms, err := svc.CreateRooms(context.Background(), who("host"), sess.SessionID, []string{"Team A", "Team B"})
	if err != nil || len(rooms) != 2 {
		t.Fatalf("create rooms: %v %+v", err, rooms)
	}
	// assign "a" into Team A
	if err := svc.Assign(context.Background(), who("host"), sess.SessionID, "a", &rooms[0].ID); err != nil {
		t.Fatalf("assign: %v", err)
	}
	me, err := svc.Me(context.Background(), who("a"), sess.SessionID)
	if err != nil || me.RoomID == nil || *me.RoomID != rooms[0].ID || me.Room != rooms[0].Room || me.JoinToken != "token:"+rooms[0].Room {
		t.Fatalf("me in breakout: %v %+v", err, me)
	}
	// a participant with no assignment lands in the main room
	me2, _ := svc.Me(context.Background(), who("b"), sess.SessionID)
	if me2.RoomID != nil || me2.Room != "main" {
		t.Fatalf("unassigned should be main: %+v", me2)
	}
	// status shows one member in Team A
	st, _ := svc.Status(context.Background(), who("host"), sess.SessionID)
	if len(st.Rooms) != 2 || st.Rooms[0].Members != 1 {
		t.Fatalf("room counts: %+v", st.Rooms)
	}
	// assigning to a bogus room 404s
	bogus := "nope"
	if err := svc.Assign(context.Background(), who("host"), sess.SessionID, "a", &bogus); codeOf(t, err) != "ROOM_NOT_FOUND" {
		t.Fatal("bogus room should 404")
	}
}

func TestCloseRoomsReturnsToMain(t *testing.T) {
	svc, _, _ := newSvc()
	sess, _ := svc.Open(context.Background(), who("host"), "main")
	rooms, _ := svc.CreateRooms(context.Background(), who("host"), sess.SessionID, []string{"A"})
	_ = svc.Assign(context.Background(), who("host"), sess.SessionID, "a", &rooms[0].ID)

	if err := svc.CloseRooms(context.Background(), who("host"), sess.SessionID); err != nil {
		t.Fatalf("close: %v", err)
	}
	me, _ := svc.Me(context.Background(), who("a"), sess.SessionID)
	if me.RoomID != nil || me.Room != "main" {
		t.Fatalf("after close everyone returns to main: %+v", me)
	}
	st, _ := svc.Status(context.Background(), who("host"), sess.SessionID)
	if len(st.Rooms) != 0 {
		t.Fatalf("no open rooms after close: %+v", st.Rooms)
	}
}

func TestEgressLifecycle(t *testing.T) {
	svc, _, egr := newSvc()
	sess, _ := svc.Open(context.Background(), who("host"), "main")

	if err := svc.StartEgress(context.Background(), who("host"), sess.SessionID, "rtmp", "not a url"); codeOf(t, err) != "VALIDATION_EGRESS_URL" {
		t.Fatal("bad url should validate")
	}
	if err := svc.StartEgress(context.Background(), who("host"), sess.SessionID, "rtmp", "rtmp://a.rtmp.example/live/key"); err != nil {
		t.Fatalf("start egress: %v", err)
	}
	if len(egr.started) != 1 || egr.started[0] != "main" {
		t.Fatalf("egress should target main room: %+v", egr.started)
	}
	st, _ := svc.Status(context.Background(), who("host"), sess.SessionID)
	if st.Egress.State != "live" || st.Egress.Kind != "rtmp" {
		t.Fatalf("status live: %+v", st.Egress)
	}
	// double-start conflicts
	if err := svc.StartEgress(context.Background(), who("host"), sess.SessionID, "rtmp", "rtmp://a/live/k"); codeOf(t, err) != "STATE_EGRESS_LIVE" {
		t.Fatal("second start should conflict")
	}
	if err := svc.StopEgress(context.Background(), who("host"), sess.SessionID); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if len(egr.stopped) != 1 {
		t.Fatalf("stop should call egress: %+v", egr.stopped)
	}
	st2, _ := svc.Status(context.Background(), who("host"), sess.SessionID)
	if st2.Egress.State != "off" {
		t.Fatalf("status off after stop: %+v", st2.Egress)
	}
}

func TestRecordingConsent(t *testing.T) {
	svc, _, _ := newSvc()
	sess, _ := svc.Open(context.Background(), who("host"), "main")

	// non-host can't request recording
	if err := svc.RequestRecording(context.Background(), who("x"), sess.SessionID); codeOf(t, err) != "LIVE_NOT_FOUND" {
		t.Fatal("non-host request should 404")
	}
	if err := svc.RequestRecording(context.Background(), who("host"), sess.SessionID); err != nil {
		t.Fatalf("request: %v", err)
	}
	_ = svc.Consent(context.Background(), who("a"), sess.SessionID, true)
	_ = svc.Consent(context.Background(), who("b"), sess.SessionID, false)
	st, _ := svc.Status(context.Background(), who("host"), sess.SessionID)
	if st.Recording.State != "requested" || st.Recording.Consent.Consented != 1 || st.Recording.Consent.Declined != 1 {
		t.Fatalf("consent tally: %+v", st.Recording)
	}
	if err := svc.StartRecording(context.Background(), who("host"), sess.SessionID); err != nil {
		t.Fatalf("start recording: %v", err)
	}
	st2, _ := svc.Status(context.Background(), who("host"), sess.SessionID)
	if st2.Recording.State != "active" {
		t.Fatalf("recording active: %+v", st2.Recording)
	}
	// a fresh request clears prior decisions
	_ = svc.RequestRecording(context.Background(), who("host"), sess.SessionID)
	st3, _ := svc.Status(context.Background(), who("host"), sess.SessionID)
	if st3.Recording.Consent.Total != 0 {
		t.Fatalf("consents should reset: %+v", st3.Recording.Consent)
	}
}
