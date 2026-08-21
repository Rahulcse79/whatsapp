// Package bots is the bot-framework control plane (T13.02): a webhook API. A user
// registers a bot (public @handle + an https webhook), gets a shared secret, and
// the server delivers HMAC-signed events to the bot's webhook when users interact
// with it. Interactive messages (buttons/cards) a bot sends are server-visible by
// design — talking to a bot is opting out of E2EE for that thread; user↔user
// interactive messages stay E2EE on the client. Outbound delivery to a live bot
// rides a Dispatcher port (HTTP adapter); the message-routing trigger is a seam.
package bots

import (
	"context"
	"errors"
	"time"
)

var (
	ErrNotFound    = errors.New("bots: not found")
	ErrHandleTaken = errors.New("bots: handle already in use")
)

// Bot is a registered bot integration.
type Bot struct {
	ID         string
	OwnerID    string
	Handle     string
	Name       string
	WebhookURL string
	Secret     string // shared HMAC secret (only surfaced on register/rotate)
	CreatedAt  time.Time
}

// BotView is a bot over the wire (no secret).
type BotView struct {
	ID          string `json:"id"`
	Handle      string `json:"handle"`
	Name        string `json:"name"`
	WebhookURL  string `json:"webhook_url"`
	CreatedAtMS int64  `json:"created_at_ms"`
}

// RegisterResult returns the new bot + its secret, shown once.
type RegisterResult struct {
	Bot    BotView `json:"bot"`
	Secret string  `json:"secret"`
}

// Event is what the server POSTs (HMAC-signed) to a bot's webhook.
type Event struct {
	Type           string `json:"type"` // message | callback
	BotID          string `json:"bot_id"`
	UserID         string `json:"user_id,omitempty"`
	ConversationID string `json:"conversation_id,omitempty"`
	Text           string `json:"text,omitempty"`
	Payload        string `json:"payload,omitempty"` // a tapped button's payload
	AtMS           int64  `json:"at_ms"`
}

// Store persists bots.
type Store interface {
	Create(ctx context.Context, b Bot) error
	Get(ctx context.Context, id string) (Bot, error)             // ErrNotFound
	GetByHandle(ctx context.Context, handle string) (Bot, error) // ErrNotFound
	ListByOwner(ctx context.Context, ownerID string) ([]Bot, error)
	Delete(ctx context.Context, ownerID, id string) error
	SetSecret(ctx context.Context, id, secret string) error
}

// Dispatcher delivers a signed event to a bot's webhook URL. The HTTP adapter
// POSTs the payload with an X-WA-Signature header; a no-op backs dev/tests.
type Dispatcher interface {
	Deliver(ctx context.Context, url, signature string, payload []byte) error
}
