// Package domain holds contacts' pure logic: the enumeration-defense limits
// (threat-model T11), handle/query normalization, and invite-capability
// validity. No I/O (depguard domain-stays-pure).
package domain

import (
	"regexp"
	"strings"
	"time"
)

const (
	// MaxSyncHandles caps one sync call (T11: ≤ 5k hashes/call).
	MaxSyncHandles = 5000
	// SyncDailyLimit is the per-user daily sync cap (T11: 4/day).
	SyncDailyLimit = 4
	// MinUsernameQuery is the shortest username search accepted (anti-scrape).
	MinUsernameQuery = 2
	// MaxSearchResults bounds a username search response.
	MaxSearchResults = 20
	// InviteTTL is how long an invite-a-friend link stays valid.
	InviteTTL = 30 * 24 * time.Hour
	// DefaultInviteMaxUses caps redemptions of a personal invite (0 = unlimited).
	DefaultInviteMaxUses = 100
)

// reE164 matches the same E.164 shape auth accepts at registration, so a synced
// handle can hash to a registered user's phone_hash (auth.Service.PhoneHash).
var reE164 = regexp.MustCompile(`^\+[1-9][0-9]{6,14}$`)

// NormalizeHandle trims a submitted phone handle and reports whether it is a
// valid E.164 number. The trimmed form is what gets peppered-hashed; auth's
// PhoneHash lowercases+trims too, so discovery joins agree.
func NormalizeHandle(h string) (string, bool) {
	n := strings.TrimSpace(h)
	return n, reE164.MatchString(n)
}

// NormalizeQuery trims a username search query and reports whether it meets the
// minimum length (a 1-char query would scrape the directory).
func NormalizeQuery(q string) (string, bool) {
	n := strings.TrimSpace(q)
	return n, len([]rune(n)) >= MinUsernameQuery
}

// Invite is a personal invite-a-friend capability (FR-CONT-03): an unguessable
// token with expiry, max-uses, and revocation (capability semantics, T12).
type Invite struct {
	Token     string
	InviterID string
	ExpiresAt time.Time
	MaxUses   int // 0 = unlimited
	Uses      int
	RevokedAt *time.Time
}

// Valid reports whether the invite can still be redeemed at `now`.
func (i Invite) Valid(now time.Time) bool {
	if i.RevokedAt != nil {
		return false
	}
	if !now.Before(i.ExpiresAt) {
		return false
	}
	if i.MaxUses > 0 && i.Uses >= i.MaxUses {
		return false
	}
	return true
}
