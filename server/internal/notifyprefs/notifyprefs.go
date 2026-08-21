// Package notifyprefs is the multi-channel notification control plane (T14.01):
// server-authoritative notification preferences (which channels, quiet hours,
// sound/vibrate), per-conversation snooze, and scheduled reminder notifications,
// plus content-free email/SMS nudge drivers. It decides WHETHER and by WHICH
// channel a wake/nudge is delivered — never the message content, which stays
// E2EE (FR-NOTIF-01). The device push pipeline lives in internal/notify; this
// context owns the preference + fallback-channel concern.
package notifyprefs

import (
	"context"
	"errors"
	"time"

	"github.com/whatsapp-v2/server/internal/notifyprefs/domain"
)

var (
	ErrNotFound = errors.New("notifyprefs: not found")
)

// Snooze is a per-conversation mute-until for one user.
type Snooze struct {
	ConversationID string
	Until          time.Time
}

// ScheduledNotification is a content-free reminder a user schedules to itself.
type ScheduledNotification struct {
	ID             string
	UserID         string
	ConversationID string // optional deep-link target ("" when none)
	Title          string
	DueAt          time.Time
	FiredAt        *time.Time // nil until the due-scan fires it
	CreatedAt      time.Time
}

// ScheduledView is a scheduled notification over the wire.
type ScheduledView struct {
	ID             string `json:"id"`
	ConversationID string `json:"conversation_id,omitempty"`
	Title          string `json:"title"`
	DueAtMS        int64  `json:"due_at_ms"`
	Fired          bool   `json:"fired"`
}

// Store persists preferences, snoozes, and scheduled notifications.
type Store interface {
	// GetPrefs returns a user's prefs, or ErrNotFound when unset (caller falls
	// back to domain.DefaultPrefs).
	GetPrefs(ctx context.Context, userID string) (domain.Prefs, error)
	UpsertPrefs(ctx context.Context, userID string, p domain.Prefs) error

	SetSnooze(ctx context.Context, userID, conversationID string, until time.Time) error
	ClearSnooze(ctx context.Context, userID, conversationID string) error
	// GetSnooze returns the snooze-until, or the zero time when not snoozed.
	GetSnooze(ctx context.Context, userID, conversationID string) (time.Time, error)

	CreateScheduled(ctx context.Context, n ScheduledNotification) error
	ListScheduled(ctx context.Context, userID string) ([]ScheduledNotification, error)
	DeleteScheduled(ctx context.Context, userID, id string) error
	// DueBefore returns pending (not-yet-fired) reminders whose due time has
	// passed, oldest first, capped at limit — the due-scan lane.
	DueBefore(ctx context.Context, cutoff time.Time, limit int) ([]ScheduledNotification, error)
	MarkFired(ctx context.Context, id string, at time.Time) error
}

// Nudge is a content-free out-of-band notification (email/SMS): it tells a user
// there is new activity and to open the app — it carries NO message content.
type Nudge struct {
	Kind  domain.Kind
	Title string // e.g. "New messages on WhatsApp V2" — generic, content-free
}

// NudgeSender delivers a Nudge to a destination address (an email or an E.164
// phone number). Destination resolution (from the user) is the caller's concern
// — mirroring how the push pipeline separates token resolution from sending.
type NudgeSender interface {
	Send(ctx context.Context, destination string, n Nudge) error
	Channel() domain.Channel
}
