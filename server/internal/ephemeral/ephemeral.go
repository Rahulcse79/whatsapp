// Package ephemeral is the minimal server-side support for disappearing messages
// (T10.01). The authoritative behaviour is client-side + E2EE (per-chat timers,
// self-destruct, view-once all ride the sealed body); the server keeps only a
// coarse per-conversation timer so a fresh device can learn the setting and the
// relay buffer (message_inbox) can be purged early as a backstop. The server
// never sees message content.
package ephemeral

import (
	"context"
	"time"
)

// Timer is a conversation's disappearing setting.
type Timer struct {
	ConversationID string
	TTLSeconds     int
	UpdatedBy      string
	UpdatedAt      time.Time
}

// TimerView is GET /v1/conversations/{id}/disappearing.
type TimerView struct {
	TTLSeconds int `json:"ttl_seconds"`
}

// Store persists per-conversation timers, gates on membership, and purges the
// relay buffer for disappearing conversations (the backstop sweep).
type Store interface {
	// IsMember reports whether the user belongs to the conversation (direct or
	// group) — the authz gate for reading/setting its timer.
	IsMember(ctx context.Context, conversationID, userID string) (bool, error)
	GetTimer(ctx context.Context, conversationID string) (int, error) // 0 if unset
	SetTimer(ctx context.Context, conversationID string, ttlSeconds int, updatedBy string, at time.Time) error
	// PurgeExpired deletes relay ciphertext for conversations with a timer, past
	// their TTL. Returns the number of rows removed. A low-frequency backstop —
	// the client enforces the UX; a device that never syncs still can't hoard.
	PurgeExpired(ctx context.Context, now time.Time) (int, error)
}
