package breakout

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/breakout/domain"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
	"github.com/whatsapp-v2/server/internal/platform/id"
)

// Service runs the advanced live-session control plane: breakout rooms, streaming
// egress, and recording consent. Host-only actions 404 for non-hosts (no probing,
// mirroring webinar/communities).
type Service struct {
	store  Store
	minter Minter
	egress Egress
	now    func() time.Time
	newID  func() string
}

func NewService(store Store, minter Minter, egress Egress) *Service {
	return &Service{store: store, minter: minter, egress: egress, now: time.Now, newID: id.New}
}

// Open starts a live session over an existing (call/webinar) main room, with the
// caller as host.
func (s *Service) Open(ctx context.Context, ident auth.Identity, mainRoom string) (OpenResult, error) {
	room := strings.TrimSpace(mainRoom)
	if room == "" {
		room = "live-" + s.newID()
	}
	sess := Session{ID: s.newID(), HostID: ident.UserID, MainRoom: room, CreatedAt: s.now()}
	if err := s.store.CreateSession(ctx, sess); err != nil {
		return OpenResult{}, httpx.Transient()
	}
	return OpenResult{SessionID: sess.ID, MainRoom: sess.MainRoom}, nil
}

// Status is the session snapshot every participant polls: egress + recording
// state (with consent tally) + breakout rooms + the caller's current room.
func (s *Service) Status(ctx context.Context, ident auth.Identity, sessionID string) (StatusResult, error) {
	sess, err := s.load(ctx, sessionID)
	if err != nil {
		return StatusResult{}, err
	}
	rooms, counts, err := s.roomsWithCounts(ctx, sessionID)
	if err != nil {
		return StatusResult{}, err
	}
	consents, err := s.store.ListConsents(ctx, sessionID)
	if err != nil {
		return StatusResult{}, httpx.Transient()
	}
	decisions := make([]domain.Decision, len(consents))
	for i, c := range consents {
		decisions[i] = domain.Decision{Decided: c.Decided, Consented: c.Consented}
	}
	res := StatusResult{
		SessionID: sess.ID,
		MainRoom:  sess.MainRoom,
		Ended:     sess.EndedAt != nil,
		Egress:    EgressView{State: sess.EgressState.String()},
		Recording: RecordingView{State: sess.Recording.String(), Consent: domain.Tally(decisions)},
		Rooms:     roomViews(rooms, counts),
		MyRoom:    s.myRoom(ctx, sessionID, ident.UserID),
	}
	if sess.EgressState == domain.EgressLive {
		res.Egress.Kind = sess.EgressKind.String()
		res.Egress.URL = sess.EgressURL
	}
	return res, nil
}

// Me returns the caller's current room and a fresh join token for it — how a
// participant (re)joins after the host moves them into or out of a breakout.
func (s *Service) Me(ctx context.Context, ident auth.Identity, sessionID string) (MeResult, error) {
	sess, err := s.load(ctx, sessionID)
	if err != nil {
		return MeResult{}, err
	}
	rooms, err := s.store.ListRooms(ctx, sessionID)
	if err != nil {
		return MeResult{}, httpx.Transient()
	}
	roomID := s.assignmentRoomID(ctx, sessionID, ident.UserID)
	lkRoom, ok := resolveRoom(sess, rooms, roomID)
	if !ok { // the assigned breakout was closed — fall back to main
		roomID, lkRoom = nil, sess.MainRoom
	}
	token, err := s.minter.MintJoin(lkRoom, ident.UserID, true)
	if err != nil {
		return MeResult{}, httpx.Transient()
	}
	return MeResult{RoomID: roomID, Room: lkRoom, JoinToken: token}, nil
}

// ── breakout rooms ───────────────────────────────────────────────────────────

// CreateRooms opens breakout rooms (host-only).
func (s *Service) CreateRooms(ctx context.Context, ident auth.Identity, sessionID string, names []string) ([]RoomView, error) {
	if err := s.requireHost(ctx, sessionID, ident.UserID); err != nil {
		return nil, err
	}
	if err := domain.ValidateRoomCount(len(names)); err != nil {
		return nil, httpx.Reject(http.StatusBadRequest, "VALIDATION_ROOMS", err.Error())
	}
	for _, n := range names {
		if err := domain.ValidateRoomName(n); err != nil {
			return nil, httpx.Reject(http.StatusBadRequest, "VALIDATION_ROOM_NAME", err.Error())
		}
	}
	now := s.now()
	out := make([]RoomView, 0, len(names))
	for _, n := range names {
		r := Room{ID: s.newID(), SessionID: sessionID, Name: strings.TrimSpace(n), Room: "bo-" + s.newID(), CreatedAt: now}
		if err := s.store.CreateRoom(ctx, r); err != nil {
			return nil, httpx.Transient()
		}
		out = append(out, RoomView{ID: r.ID, Name: r.Name, Room: r.Room})
	}
	return out, nil
}

// Assign moves a participant into a breakout room (roomID) or back to the main
// room (roomID nil). Host-only. The target then re-fetches Me for a token.
func (s *Service) Assign(ctx context.Context, ident auth.Identity, sessionID, userID string, roomID *string) error {
	if err := s.requireHost(ctx, sessionID, ident.UserID); err != nil {
		return err
	}
	if roomID != nil {
		rooms, err := s.store.ListRooms(ctx, sessionID)
		if err != nil {
			return httpx.Transient()
		}
		if !roomExists(rooms, *roomID) {
			return httpx.Reject(http.StatusNotFound, "ROOM_NOT_FOUND", "breakout room not found")
		}
	}
	if err := s.store.SetAssignment(ctx, sessionID, userID, roomID, s.now()); err != nil {
		return httpx.Transient()
	}
	return nil
}

// CloseRooms closes every breakout room and returns everyone to the main room
// (host-only).
func (s *Service) CloseRooms(ctx context.Context, ident auth.Identity, sessionID string) error {
	if err := s.requireHost(ctx, sessionID, ident.UserID); err != nil {
		return err
	}
	if err := s.store.CloseRooms(ctx, sessionID, s.now()); err != nil {
		return httpx.Transient()
	}
	if err := s.store.ClearAssignments(ctx, sessionID); err != nil {
		return httpx.Transient()
	}
	return nil
}

// ── streaming egress ─────────────────────────────────────────────────────────

// StartEgress begins streaming the main room out to an RTMP/HLS target (host-only).
func (s *Service) StartEgress(ctx context.Context, ident auth.Identity, sessionID, kindStr, target string) error {
	if err := s.requireHost(ctx, sessionID, ident.UserID); err != nil {
		return err
	}
	sess, err := s.load(ctx, sessionID)
	if err != nil {
		return err
	}
	kind, ok := domain.ParseEgressKind(kindStr)
	if !ok {
		return httpx.Reject(http.StatusBadRequest, "VALIDATION_EGRESS_KIND", "kind must be rtmp or hls")
	}
	if err := domain.ValidateEgressTarget(kind, target); err != nil {
		return httpx.Reject(http.StatusBadRequest, "VALIDATION_EGRESS_URL", err.Error())
	}
	if sess.EgressState == domain.EgressLive {
		return httpx.Reject(http.StatusConflict, "STATE_EGRESS_LIVE", "already streaming; stop first")
	}
	ref, err := s.egress.Start(ctx, sess.MainRoom, kind, strings.TrimSpace(target))
	if err != nil {
		return httpx.Reject(http.StatusBadGateway, "EGRESS_START_FAILED", "could not start the stream")
	}
	if err := s.store.SetEgress(ctx, sessionID, domain.EgressLive, kind, strings.TrimSpace(target), ref); err != nil {
		return httpx.Transient()
	}
	return nil
}

// StopEgress stops the outbound stream (host-only).
func (s *Service) StopEgress(ctx context.Context, ident auth.Identity, sessionID string) error {
	if err := s.requireHost(ctx, sessionID, ident.UserID); err != nil {
		return err
	}
	sess, err := s.load(ctx, sessionID)
	if err != nil {
		return err
	}
	if sess.EgressState != domain.EgressLive {
		return nil // idempotent
	}
	_ = s.egress.Stop(ctx, sess.EgressRef) // best-effort; we still clear local state
	if err := s.store.SetEgress(ctx, sessionID, domain.EgressOff, sess.EgressKind, "", ""); err != nil {
		return httpx.Transient()
	}
	return nil
}

// ── recording consent ────────────────────────────────────────────────────────

// RequestRecording opens the consent window (host-only): state → requested and
// prior decisions are cleared so every present participant is re-asked.
func (s *Service) RequestRecording(ctx context.Context, ident auth.Identity, sessionID string) error {
	if err := s.requireHost(ctx, sessionID, ident.UserID); err != nil {
		return err
	}
	if err := s.store.ResetConsents(ctx, sessionID); err != nil {
		return httpx.Transient()
	}
	return s.setRecording(ctx, sessionID, domain.RecordingRequested)
}

// Consent records the caller's recording decision (any participant).
func (s *Service) Consent(ctx context.Context, ident auth.Identity, sessionID string, ok bool) error {
	if _, err := s.load(ctx, sessionID); err != nil {
		return err
	}
	if err := s.store.SetConsent(ctx, sessionID, ident.UserID, ok, s.now()); err != nil {
		return httpx.Transient()
	}
	return nil
}

// StartRecording turns recording on (host-only). Clients that consented capture
// locally; every client shows the recording indicator (Status → active).
func (s *Service) StartRecording(ctx context.Context, ident auth.Identity, sessionID string) error {
	if err := s.requireHost(ctx, sessionID, ident.UserID); err != nil {
		return err
	}
	return s.setRecording(ctx, sessionID, domain.RecordingActive)
}

// StopRecording turns recording off (host-only).
func (s *Service) StopRecording(ctx context.Context, ident auth.Identity, sessionID string) error {
	if err := s.requireHost(ctx, sessionID, ident.UserID); err != nil {
		return err
	}
	return s.setRecording(ctx, sessionID, domain.RecordingOff)
}

// Close ends the session: stop egress if live, close breakouts (host-only).
func (s *Service) Close(ctx context.Context, ident auth.Identity, sessionID string) error {
	if err := s.requireHost(ctx, sessionID, ident.UserID); err != nil {
		return err
	}
	sess, err := s.load(ctx, sessionID)
	if err != nil {
		return err
	}
	if sess.EgressState == domain.EgressLive {
		_ = s.egress.Stop(ctx, sess.EgressRef)
		_ = s.store.SetEgress(ctx, sessionID, domain.EgressOff, sess.EgressKind, "", "")
	}
	_ = s.store.CloseRooms(ctx, sessionID, s.now())
	if err := s.store.EndSession(ctx, sessionID, s.now()); err != nil {
		return httpx.Transient()
	}
	return nil
}

// ── helpers ──────────────────────────────────────────────────────────────────

func (s *Service) load(ctx context.Context, sessionID string) (Session, error) {
	sess, err := s.store.GetSession(ctx, sessionID)
	if errors.Is(err, ErrNotFound) {
		return Session{}, notFound()
	}
	if err != nil {
		return Session{}, httpx.Transient()
	}
	return sess, nil
}

func (s *Service) requireHost(ctx context.Context, sessionID, userID string) error {
	sess, err := s.store.GetSession(ctx, sessionID)
	if err != nil || sess.HostID != userID {
		return notFound() // don't reveal the session to non-hosts probing host actions
	}
	return nil
}

func (s *Service) setRecording(ctx context.Context, sessionID string, state domain.RecordingState) error {
	if err := s.store.SetRecording(ctx, sessionID, state); err != nil {
		return httpx.Transient()
	}
	return nil
}

func (s *Service) roomsWithCounts(ctx context.Context, sessionID string) ([]Room, map[string]int, error) {
	rooms, err := s.store.ListRooms(ctx, sessionID)
	if err != nil {
		return nil, nil, httpx.Transient()
	}
	counts, err := s.store.CountByRoom(ctx, sessionID)
	if err != nil {
		return nil, nil, httpx.Transient()
	}
	return rooms, counts, nil
}

// assignmentRoomID returns the caller's current breakout room id (nil = main).
func (s *Service) assignmentRoomID(ctx context.Context, sessionID, userID string) *string {
	a, err := s.store.GetAssignment(ctx, sessionID, userID)
	if err != nil {
		return nil
	}
	return a.RoomID
}

func (s *Service) myRoom(ctx context.Context, sessionID, userID string) *string {
	return s.assignmentRoomID(ctx, sessionID, userID)
}

func resolveRoom(sess Session, rooms []Room, roomID *string) (string, bool) {
	if roomID == nil {
		return sess.MainRoom, true
	}
	for _, r := range rooms {
		if r.ID == *roomID {
			return r.Room, true
		}
	}
	return "", false
}

func roomExists(rooms []Room, roomID string) bool {
	for _, r := range rooms {
		if r.ID == roomID {
			return true
		}
	}
	return false
}

func roomViews(rooms []Room, counts map[string]int) []RoomView {
	out := make([]RoomView, len(rooms))
	for i, r := range rooms {
		out[i] = RoomView{ID: r.ID, Name: r.Name, Room: r.Room, Members: counts[r.ID]}
	}
	return out
}

func notFound() error {
	return httpx.Reject(http.StatusNotFound, "LIVE_NOT_FOUND", "live session not found")
}
