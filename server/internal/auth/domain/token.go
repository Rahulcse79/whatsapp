package domain

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"time"
)

// Session is a device's refresh-token chain. Rotation keeps exactly one
// valid token per session; the previous hash stays in RotatedFrom so a
// replayed old token is detectable as reuse (HLD §15.2).
type Session struct {
	ID          string
	DeviceID    string
	UserID      string // populated via the devices join
	RefreshHash []byte
	RotatedFrom []byte
	ExpiresAt   time.Time
	RevokedAt   time.Time // zero = active
}

// RefreshOutcome classifies a refresh attempt against a found session.
type RefreshOutcome int

const (
	RefreshRotate RefreshOutcome = iota
	RefreshExpired
	RefreshRevoked
)

// EvaluateSession is the pure decision for a session located by its CURRENT
// refresh hash. (Reuse detection is a lookup concern: a token matching
// RotatedFrom instead of RefreshHash is a replay — the caller revokes.)
func EvaluateSession(s Session, now time.Time) RefreshOutcome {
	if !s.RevokedAt.IsZero() {
		return RefreshRevoked
	}
	if now.After(s.ExpiresAt) {
		return RefreshExpired
	}
	return RefreshRotate
}

// NewRefreshToken draws a 256-bit token. Only its SHA-256 is ever stored —
// a database leak yields nothing replayable.
func NewRefreshToken(entropy io.Reader) (token string, hash []byte, err error) {
	raw := make([]byte, 32)
	if _, err := io.ReadFull(entropy, raw); err != nil {
		return "", nil, fmt.Errorf("refresh entropy: %w", err)
	}
	tok := base64.RawURLEncoding.EncodeToString(raw)
	return tok, HashToken(tok), nil
}

// HashToken maps a presented refresh token to its storage hash.
func HashToken(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}
