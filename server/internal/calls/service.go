// Package calls is the call control plane (rtc-lld §1/§6): room + short-lived
// join-token minting, the server-authoritative ring state machine, call history,
// and LiveKit webhook reconciliation. Media never crosses this package — clients
// carry SRTP directly to the SFU.
package calls

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/calls/domain"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
	"github.com/whatsapp-v2/server/internal/platform/id"
)

// ErrNotFound is returned by ports when no ring/room matches.
var ErrNotFound = errors.New("calls: not found")

// ── ports ────────────────────────────────────────────────────────────────

// Minter mints LiveKit join tokens (satisfied by *TokenMinter).
type Minter interface {
	Mint(g JoinGrant, ttl time.Duration, now time.Time) (string, error)
}

// RingStore persists ring state (Valkey). Save upserts with the given TTL.
type RingStore interface {
	Save(ctx context.Context, rec RingRecord, ttl time.Duration) error
	Get(ctx context.Context, ringID string) (RingRecord, error)       // ErrNotFound
	GetByRoom(ctx context.Context, roomID string) (RingRecord, error) // ErrNotFound
	ExpiredRinging(ctx context.Context, now time.Time, limit int) ([]RingRecord, error)
}

// History persists + reads call_records (PG).
type History interface {
	Upsert(ctx context.Context, rec CallRecord) error
	List(ctx context.Context, userID, cursor string, limit int) ([]CallRecord, string, error)
}

// Signaler pushes call WS frames to specific devices (via the delivery path).
type Signaler interface {
	Offer(ctx context.Context, deviceIDs []string, o CallOfferSignal) error
	Ring(ctx context.Context, deviceIDs []string, ringID string, state domain.RingState, byUserID string) error
	End(ctx context.Context, deviceIDs []string, ringID, roomID, reason string) error
}

// Pusher wakes devices out-of-band (VoIP push to ring; missed-call notification).
type Pusher interface {
	VoIP(ctx context.Context, deviceIDs []string, inv CallInvite) error
	Missed(ctx context.Context, deviceIDs []string, ringID string) error
}

// Devices resolves each user's active (non-revoked) device ids.
type Devices interface {
	DevicesOf(ctx context.Context, userIDs []string) (map[string][]string, error)
}

// Service orchestrates call setup, ring transitions, history, and webhooks.
type Service struct {
	minter   Minter
	ring     RingStore
	history  History
	signaler Signaler
	pusher   Pusher
	devices  Devices
	log      logger
	now      func() time.Time
	newID    func() string
}

type logger interface {
	Warn(msg string, args ...any)
}

func NewService(minter Minter, ring RingStore, history History, signaler Signaler, pusher Pusher, devices Devices, log logger) *Service {
	return &Service{
		minter: minter, ring: ring, history: history, signaler: signaler,
		pusher: pusher, devices: devices, log: log, now: time.Now, newID: id.New,
	}
}

// Create starts a call: mints the caller's room-scoped join token, opens the
// ring machine, and rings every callee device (WS offer + VoIP push). The room
// is created lazily by LiveKit on first join, so no admin round-trip here.
func (s *Service) Create(ctx context.Context, caller auth.Identity, calleeIDs []string, kind domain.CallKind) (CreateResult, error) {
	if err := domain.ValidateCreate(kind, len(calleeIDs)); err != nil {
		return CreateResult{}, validationError(err)
	}
	now := s.now()
	roomID := "call-" + s.newID()
	ringID := s.newID()

	token, err := s.minter.Mint(joinGrant(caller, roomID), domain.JoinTokenTTL, now)
	if err != nil {
		return CreateResult{}, httpx.Transient()
	}

	rec := RingRecord{
		RingID: ringID, RoomID: roomID, Kind: kind,
		CallerID: caller.UserID, CallerDeviceID: caller.DeviceID,
		CalleeIDs: dedupExcept(calleeIDs, caller.UserID),
		State:     domain.StateRinging, StartedAt: now, Deadline: now.Add(domain.MissedAfter),
	}
	if err := s.ring.Save(ctx, rec, domain.RingTTL); err != nil {
		return CreateResult{}, httpx.Transient()
	}

	// Ring the callees (best-effort: the caller already holds a valid token and
	// the ring is authoritative; a signaling hiccup is recovered by push/retry).
	calleeDevices := s.deviceList(ctx, rec.CalleeIDs)
	participants := append([]string{caller.UserID}, rec.CalleeIDs...)
	s.try("offer", s.signaler.Offer(ctx, calleeDevices, CallOfferSignal{
		RingID: ringID, RoomID: roomID, Kind: kind,
		CallerUserID: caller.UserID, CallerDeviceID: caller.DeviceID, ParticipantUserIDs: participants,
	}))
	s.try("voip", s.pusher.VoIP(ctx, calleeDevices, CallInvite{RingID: ringID, RoomID: roomID, Kind: kind, CallerUserID: caller.UserID}))

	return CreateResult{RoomID: roomID, JoinToken: token, RingID: ringID}, nil
}

// Answer transitions the ring to answered and mints the answerer's join token.
// The caller's devices get CallRing{ANSWERED}; the answerer's sibling devices get
// ANSWERED_ELSEWHERE. Re-answering from the same device re-issues a token.
func (s *Service) Answer(ctx context.Context, ident auth.Identity, ringID string) (string, error) {
	rec, err := s.loadRing(ctx, ringID)
	if err != nil {
		return "", err
	}
	if !contains(rec.CalleeIDs, ident.UserID) {
		return "", httpx.Reject(http.StatusForbidden, "STATE_FORBIDDEN", "not a callee of this ring")
	}

	next, ok := domain.Next(rec.State, domain.EventAnswer)
	if !ok {
		if rec.State == domain.StateAnswered && rec.AnsweredBy == ident.DeviceID {
			return s.minter.Mint(joinGrant(ident, rec.RoomID), domain.JoinTokenTTL, s.now()) // idempotent re-issue
		}
		return "", httpx.Reject(http.StatusConflict, "STATE_RING_CLOSED", "call is no longer ringing")
	}

	rec.State = next
	rec.AnsweredBy = ident.DeviceID
	if err := s.ring.Save(ctx, rec, domain.CallMaxTTL); err != nil {
		return "", httpx.Transient()
	}
	token, err := s.minter.Mint(joinGrant(ident, rec.RoomID), domain.JoinTokenTTL, s.now())
	if err != nil {
		return "", httpx.Transient()
	}

	s.try("ring-answered", s.signaler.Ring(ctx, s.deviceList(ctx, []string{rec.CallerID}), ringID, domain.StateAnswered, ident.UserID))
	s.try("ring-elsewhere", s.signaler.Ring(ctx, s.siblingDevices(ctx, rec.CalleeIDs, ident.DeviceID), ringID, domain.StateAnsweredElsewhere, ident.UserID))
	return token, nil
}

// Decline transitions the ring to declined and notifies the caller.
func (s *Service) Decline(ctx context.Context, ident auth.Identity, ringID string) error {
	rec, err := s.loadRing(ctx, ringID)
	if err != nil {
		return err
	}
	if !contains(rec.CalleeIDs, ident.UserID) {
		return httpx.Reject(http.StatusForbidden, "STATE_FORBIDDEN", "not a callee of this ring")
	}
	next, ok := domain.Next(rec.State, domain.EventDecline)
	if !ok {
		return nil // already terminal — idempotent
	}
	rec.State = next
	if err := s.ring.Save(ctx, rec, domain.RingTTL); err != nil {
		return httpx.Transient()
	}
	s.try("ring-declined", s.signaler.Ring(ctx, s.deviceList(ctx, []string{rec.CallerID}), ringID, domain.StateDeclined, ident.UserID))
	s.recordOutcome(ctx, rec, OutcomeDeclined)
	return nil
}

// Rejoin mints a fresh token for a participant of an active call (ICE/network
// recovery). Only a participant of a still-live ring may rejoin.
func (s *Service) Rejoin(ctx context.Context, ident auth.Identity, roomID string) (string, error) {
	rec, err := s.ring.GetByRoom(ctx, roomID)
	if errors.Is(err, ErrNotFound) {
		return "", httpx.Reject(http.StatusNotFound, "CALL_NOT_FOUND", "no such active call")
	}
	if err != nil {
		return "", httpx.Transient()
	}
	if ident.UserID != rec.CallerID && !contains(rec.CalleeIDs, ident.UserID) {
		return "", httpx.Reject(http.StatusForbidden, "STATE_FORBIDDEN", "not a participant of this call")
	}
	if rec.State.Terminal() {
		return "", httpx.Reject(http.StatusConflict, "STATE_CALL_ENDED", "call has ended")
	}
	return s.minter.Mint(joinGrant(ident, roomID), domain.JoinTokenTTL, s.now())
}

// History returns the caller's call records (metadata only).
func (s *Service) History(ctx context.Context, ident auth.Identity, cursor string, limit int) ([]CallRecord, string, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	recs, next, err := s.history.List(ctx, ident.UserID, cursor, limit)
	if err != nil {
		return nil, "", httpx.Transient()
	}
	return recs, next, nil
}

// SweepMissed transitions rings whose deadline elapsed to missed, notifying the
// caller and pushing a missed-call to the callees. Idempotent; run on a ticker
// (the ring machine's server-authoritative timeout).
func (s *Service) SweepMissed(ctx context.Context, limit int) (int, error) {
	expired, err := s.ring.ExpiredRinging(ctx, s.now(), limit)
	if err != nil {
		return 0, err
	}
	missed := 0
	for _, rec := range expired {
		next, ok := domain.Next(rec.State, domain.EventMiss)
		if !ok {
			continue
		}
		rec.State = next
		if err := s.ring.Save(ctx, rec, domain.RingTTL); err != nil {
			s.log.Warn("calls: persisting missed transition failed", "ring", rec.RingID, "err", err)
			continue
		}
		s.try("ring-missed", s.signaler.Ring(ctx, s.deviceList(ctx, []string{rec.CallerID}), rec.RingID, domain.StateMissed, ""))
		s.try("push-missed", s.pusher.Missed(ctx, s.deviceList(ctx, rec.CalleeIDs), rec.RingID))
		s.recordOutcome(ctx, rec, OutcomeMissed)
		missed++
	}
	return missed, nil
}

// HandleWebhook reconciles a verified LiveKit webhook. Idempotent (LiveKit
// redelivers): a room that finished while its ring is still open is a zombie —
// force-end it and record history.
func (s *Service) HandleWebhook(ctx context.Context, ev WebhookEvent) error {
	switch ev.Event {
	case "room_finished":
		rec, err := s.ring.GetByRoom(ctx, ev.Room.Name)
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if !rec.State.Terminal() {
			rec.State = domain.StateEnded
			if err := s.ring.Save(ctx, rec, domain.RingTTL); err != nil {
				return err
			}
			s.try("end", s.signaler.End(ctx, s.deviceList(ctx, append([]string{rec.CallerID}, rec.CalleeIDs...)), rec.RingID, rec.RoomID, "room_finished"))
		}
		outcome := OutcomeCompleted
		if !rec.answered() {
			outcome = OutcomeFailed
		}
		s.recordOutcome(ctx, rec, outcome)
	case "participant_joined":
		// A callee media-joined without the /answer REST (e.g. answered on media
		// first): reconcile the ring to answered so siblings/caller learn of it.
		rec, err := s.ring.GetByRoom(ctx, ev.Room.Name)
		if err != nil {
			return nil // room may already be gone (ring TTL / not found) — nothing to reconcile
		}
		if next, ok := domain.Next(rec.State, domain.EventAnswer); ok {
			rec.State = next
			_ = s.ring.Save(ctx, rec, domain.CallMaxTTL)
		}
	}
	return nil
}

// ── helpers ──────────────────────────────────────────────────────────────

func (s *Service) loadRing(ctx context.Context, ringID string) (RingRecord, error) {
	rec, err := s.ring.Get(ctx, ringID)
	if errors.Is(err, ErrNotFound) {
		return RingRecord{}, httpx.Reject(http.StatusNotFound, "RING_NOT_FOUND", "no such ring")
	}
	if err != nil {
		return RingRecord{}, httpx.Transient()
	}
	return rec, nil
}

func (s *Service) recordOutcome(ctx context.Context, rec RingRecord, outcome Outcome) {
	now := s.now()
	participants := append([]string{rec.CallerID}, rec.CalleeIDs...)
	started := rec.StartedAt
	if err := s.history.Upsert(ctx, CallRecord{
		ID: rec.RingID, RoomID: rec.RoomID, Kind: rec.Kind, Initiator: rec.CallerID,
		Participants: participants, StartedAt: &started, EndedAt: &now, Outcome: outcome,
	}); err != nil {
		s.log.Warn("calls: persisting call record failed", "ring", rec.RingID, "err", err)
	}
}

// deviceList resolves all device ids for a set of users (empty on error — the
// ring stays authoritative and push/retry recovers).
func (s *Service) deviceList(ctx context.Context, userIDs []string) []string {
	if len(userIDs) == 0 {
		return nil
	}
	byUser, err := s.devices.DevicesOf(ctx, userIDs)
	if err != nil {
		s.log.Warn("calls: resolving devices failed", "err", err)
		return nil
	}
	var out []string
	for _, ds := range byUser {
		out = append(out, ds...)
	}
	return out
}

// siblingDevices are the answering callee's OTHER devices (all callee devices
// minus the one that answered) — they get ANSWERED_ELSEWHERE.
func (s *Service) siblingDevices(ctx context.Context, calleeIDs []string, exceptDevice string) []string {
	all := s.deviceList(ctx, calleeIDs)
	out := all[:0]
	for _, d := range all {
		if d != exceptDevice {
			out = append(out, d)
		}
	}
	return out
}

func (s *Service) try(what string, err error) {
	if err != nil {
		s.log.Warn("calls: signaling step failed", "step", what, "err", err)
	}
}

func joinGrant(ident auth.Identity, room string) JoinGrant {
	return JoinGrant{Identity: ident.UserID + ":" + ident.DeviceID, Room: room, CanPublish: true, CanSubscribe: true}
}

func validationError(err error) error {
	switch {
	case errors.Is(err, domain.ErrBadKind):
		return httpx.Reject(http.StatusBadRequest, "VALIDATION_KIND", "kind must be voice or video")
	case errors.Is(err, domain.ErrNoCallees):
		return httpx.Reject(http.StatusBadRequest, "VALIDATION_CALLEES", "at least one callee required")
	case errors.Is(err, domain.ErrTooManyCallees):
		return httpx.Reject(http.StatusBadRequest, "VALIDATION_CALLEES", "too many callees (max 31)")
	default:
		return httpx.Reject(http.StatusBadRequest, "VALIDATION", "invalid request")
	}
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// dedupExcept returns xs with duplicates and `except` removed, order preserved.
func dedupExcept(xs []string, except string) []string {
	seen := map[string]bool{except: true}
	out := make([]string, 0, len(xs))
	for _, x := range xs {
		if x == "" || seen[x] {
			continue
		}
		seen[x] = true
		out = append(out, x)
	}
	return out
}
