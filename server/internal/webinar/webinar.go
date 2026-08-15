// Package webinar is the live-mode control plane (T9.02): 1-to-many broadcasts
// with a waiting room, raise-hand, promote-to-speaker, Q&A, and attendance
// reports. Media rides the existing LiveKit plane; role-scoped join tokens
// (canPublish) enforce the 1-to-many shape. In-call polls reuse internal/polls;
// live captions + translation are on-device (client, @wa/call-engine).
package webinar

import (
	"context"
	"errors"
	"time"

	"github.com/whatsapp-v2/server/internal/webinar/domain"
)

var ErrNotFound = errors.New("webinar: not found")

// Webinar is a live session's record.
type Webinar struct {
	ID        string
	Title     string
	HostID    string
	RoomID    string
	CreatedAt time.Time
	EndedAt   *time.Time
}

// Participant is one attendee/speaker/host with waiting-room + attendance state.
type Participant struct {
	UserID     string
	Role       domain.Role
	Status     domain.Status
	HandRaised bool
	JoinedAt   time.Time
	LeftAt     *time.Time
}

// Question is a Q&A entry.
type Question struct {
	ID        string
	AskerID   string
	Body      string
	Upvotes   int
	Answered  bool
	CreatedAt time.Time
}

// CreateResult is POST /v1/webinars (host).
type CreateResult struct {
	WebinarID string `json:"webinar_id"`
	RoomID    string `json:"room_id"`
	JoinToken string `json:"join_token"` // host — publish
}

// MeResult is GET /v1/webinars/{id}/me — the caller's live state + a token once
// admitted (publish per role).
type MeResult struct {
	Status     string `json:"status"`
	Role       string `json:"role"`
	HandRaised bool   `json:"hand_raised"`
	RoomID     string `json:"room_id"`
	JoinToken  string `json:"join_token,omitempty"` // present only when admitted
	CanPublish bool   `json:"can_publish"`
}

// RosterEntry is one row of the host roster / attendance report.
type RosterEntry struct {
	UserID     string `json:"user_id"`
	Role       string `json:"role"`
	Status     string `json:"status"`
	HandRaised bool   `json:"hand_raised"`
	JoinedAtMS int64  `json:"joined_at_ms"`
	LeftAtMS   int64  `json:"left_at_ms,omitempty"`
}

// QuestionView is one Q&A entry over the wire.
type QuestionView struct {
	ID          string `json:"id"`
	AskerID     string `json:"asker_id"`
	Body        string `json:"body"`
	Upvotes     int    `json:"upvotes"`
	Answered    bool   `json:"answered"`
	CreatedAtMS int64  `json:"created_at_ms"`
}

// Minter mints a role-scoped LiveKit join token (publish for host/speakers,
// subscribe-only for attendees). Implemented over calls.TokenMinter.
type Minter interface {
	MintJoin(room, identity string, canPublish bool) (string, error)
}

// Store persists webinars + participants + questions.
type Store interface {
	Create(ctx context.Context, w Webinar) error
	Get(ctx context.Context, id string) (Webinar, error) // ErrNotFound
	End(ctx context.Context, id string, at time.Time) error

	UpsertParticipant(ctx context.Context, webinarID string, p Participant) error
	GetParticipant(ctx context.Context, webinarID, userID string) (Participant, error) // ErrNotFound
	SetStatus(ctx context.Context, webinarID, userID string, status domain.Status, leftAt *time.Time) error
	SetRole(ctx context.Context, webinarID, userID string, role domain.Role) error
	SetHand(ctx context.Context, webinarID, userID string, raised bool) error
	ListParticipants(ctx context.Context, webinarID string) ([]Participant, error)

	CreateQuestion(ctx context.Context, webinarID string, q Question) error
	ListQuestions(ctx context.Context, webinarID string) ([]Question, error)
	UpvoteQuestion(ctx context.Context, webinarID, questionID, voterID string) error
	AnswerQuestion(ctx context.Context, webinarID, questionID string) error
}
