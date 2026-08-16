// Package domain holds the disappearing-messages pure logic (T10.01): the
// per-conversation timer bounds. The timer VALUE is E2EE-propagated between
// clients (a sealed control message); the server keeps only a coarse copy so a
// fresh device can learn the setting and the relay buffer can be purged early —
// it never sees message content.
package domain

import (
	"errors"
	"strings"
)

// TTL presets (seconds), matching the client picker. 0 = off (default 30-day
// relay TTL applies). Callers may pass any value in [0, MaxTTL]; these are the
// well-known ones.
const (
	TTLOff    = 0
	TTL24Hour = 86_400
	TTL7Day   = 604_800
	TTL90Day  = 7_776_000 // also the message_inbox relay backstop
	MaxTTL    = TTL90Day
)

var ErrBadTTL = errors.New("ephemeral: ttl must be between 0 and 90 days (seconds)")

// ValidateTTL bounds a disappearing timer.
func ValidateTTL(seconds int) error {
	if seconds < 0 || seconds > MaxTTL {
		return ErrBadTTL
	}
	return nil
}

// LabelFor returns a short human label for a timer (for logs/tests).
func LabelFor(seconds int) string {
	switch seconds {
	case TTLOff:
		return "off"
	case TTL24Hour:
		return "24h"
	case TTL7Day:
		return "7d"
	case TTL90Day:
		return "90d"
	default:
		return "custom"
	}
}

// NormalizeConversationID trims a conversation id (defensive; validation of the
// uuid shape is the storage layer's job).
func NormalizeConversationID(id string) string { return strings.TrimSpace(id) }
