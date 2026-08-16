// Package breakout is the advanced live-session control plane (T9.03): breakout
// rooms, live streaming out (RTMP/HLS egress via LiveKit), and client-side
// recording with consent signalling. It sits over the same LiveKit media plane
// as calls/webinar — breakout participants get a fresh role-scoped join token per
// room; egress + recording are host-driven and never touch E2EE payloads (the
// SFU forwards ciphertext, recording happens on-device only with consent).
// Multi-camera + 4K profiles are purely client-side (@wa/call-engine).
package breakout

import (
	"context"
	"errors"
	"time"

	"github.com/whatsapp-v2/server/internal/breakout/domain"
)

var ErrNotFound = errors.New("breakout: not found")

// Session is a live session owned by a host, over a main LiveKit room, carrying
// breakout/egress/recording state.
type Session struct {
	ID          string
	HostID      string
	MainRoom    string
	CreatedAt   time.Time
	EndedAt     *time.Time
	EgressState domain.EgressState
	EgressKind  domain.EgressKind
	EgressURL   string
	EgressRef   string // opaque egress id from the LiveKit egress API (to stop)
	Recording   domain.RecordingState
}

// Room is a breakout room within a session (its own LiveKit room).
type Room struct {
	ID        string
	SessionID string
	Name      string
	Room      string // LiveKit room name (bo-…)
	CreatedAt time.Time
	ClosedAt  *time.Time
}

// Assignment is a participant's current room (RoomID nil = the main room).
type Assignment struct {
	UserID     string
	RoomID     *string
	AssignedAt time.Time
}

// Consent is one participant's recording decision.
type Consent struct {
	UserID    string
	Decided   bool
	Consented bool
	DecidedAt time.Time
}

// ── wire results ─────────────────────────────────────────────────────────────

// OpenResult is POST /v1/live (host).
type OpenResult struct {
	SessionID string `json:"session_id"`
	MainRoom  string `json:"main_room"`
}

// RoomView is one breakout room over the wire.
type RoomView struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Room    string `json:"room"`
	Members int    `json:"members"`
}

// MeResult is the caller's current room + a join token for it (breakout or main).
type MeResult struct {
	RoomID    *string `json:"room_id"` // nil = main room
	Room      string  `json:"room"`    // LiveKit room name to join
	JoinToken string  `json:"join_token"`
}

// StatusResult is GET /v1/live/{id} — the whole session snapshot clients poll.
type StatusResult struct {
	SessionID string        `json:"session_id"`
	MainRoom  string        `json:"main_room"`
	Ended     bool          `json:"ended"`
	Egress    EgressView    `json:"egress"`
	Recording RecordingView `json:"recording"`
	Rooms     []RoomView    `json:"rooms"`
	MyRoom    *string       `json:"my_room"` // nil = main
}

type EgressView struct {
	State string `json:"state"`
	Kind  string `json:"kind,omitempty"`
	URL   string `json:"url,omitempty"`
}

type RecordingView struct {
	State   string                `json:"state"`
	Consent domain.ConsentSummary `json:"consent"`
}

// ── ports ────────────────────────────────────────────────────────────────────

// Minter mints a LiveKit join token for a room (participants publish + subscribe
// in breakouts). Implemented over calls.TokenMinter, like webinar/ptt.
type Minter interface {
	MintJoin(room, identity string, canPublish bool) (string, error)
}

// Egress starts/stops a LiveKit room composite egress (RTMP push or HLS segment).
// The real adapter calls the LiveKit egress API; a no-op adapter backs dev/tests.
type Egress interface {
	Start(ctx context.Context, room string, kind domain.EgressKind, target string) (ref string, err error)
	Stop(ctx context.Context, ref string) error
}

// Store persists sessions + breakout rooms + assignments + recording consents.
type Store interface {
	CreateSession(ctx context.Context, s Session) error
	GetSession(ctx context.Context, id string) (Session, error) // ErrNotFound
	EndSession(ctx context.Context, id string, at time.Time) error
	SetEgress(ctx context.Context, id string, state domain.EgressState, kind domain.EgressKind, url, ref string) error
	SetRecording(ctx context.Context, id string, state domain.RecordingState) error

	CreateRoom(ctx context.Context, r Room) error
	ListRooms(ctx context.Context, sessionID string) ([]Room, error) // open rooms
	CloseRooms(ctx context.Context, sessionID string, at time.Time) error
	CountByRoom(ctx context.Context, sessionID string) (map[string]int, error) // roomID → members

	SetAssignment(ctx context.Context, sessionID, userID string, roomID *string, at time.Time) error
	GetAssignment(ctx context.Context, sessionID, userID string) (Assignment, error) // ErrNotFound
	ClearAssignments(ctx context.Context, sessionID string) error

	ResetConsents(ctx context.Context, sessionID string) error
	SetConsent(ctx context.Context, sessionID, userID string, consented bool, at time.Time) error
	ListConsents(ctx context.Context, sessionID string) ([]Consent, error)
}
