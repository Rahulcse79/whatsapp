// Package deviceauth is the device-auth hardening control plane (T10.02):
// WebAuthn passkeys as a 2FA / step-up path (register + assert, verified with Go
// stdlib crypto) and a login-event audit that flags sign-ins from a new IP.
// Biometric unlock on the client reuses the platform authenticator; secure key
// storage stays in the clients' secure stores (crypto-wrapper / Keychain).
package deviceauth

import (
	"context"
	"errors"
	"time"
)

var ErrNotFound = errors.New("deviceauth: not found")

// Credential is a registered passkey (its public key + counter).
type Credential struct {
	ID         string // credential id (base64url)
	UserID     string
	Alg        int    // COSE alg (-7 ES256 | -8 EdDSA)
	PublicKey  []byte // ES256: 64 bytes x||y; EdDSA: 32 bytes
	SignCount  uint32
	Name       string
	CreatedAt  time.Time
	LastUsedAt *time.Time
}

// Challenge is a single-use WebAuthn challenge bound to a user + ceremony.
type Challenge struct {
	Value     string // base64url; the lookup key
	UserID    string
	Purpose   string // "register" | "login"
	ExpiresAt time.Time
}

// LoginEvent is one recorded sign-in (metadata only).
type LoginEvent struct {
	ID         string
	UserID     string
	DeviceID   string
	IP         string
	UserAgent  string
	At         time.Time
	Suspicious bool
}

// ── wire results ─────────────────────────────────────────────────────────────

// RegistrationOptions is POST /v1/auth/passkeys/register/begin — the browser
// turns this into a PublicKeyCredentialCreationOptions.
type RegistrationOptions struct {
	Challenge string `json:"challenge"`
	RPID      string `json:"rp_id"`
	Origin    string `json:"origin"`
	UserID    string `json:"user_id"`
	Algs      []int  `json:"algs"` // COSE alg ids we accept (-7, -8)
}

// LoginOptions is POST /v1/auth/passkeys/login/begin.
type LoginOptions struct {
	Challenge      string   `json:"challenge"`
	RPID           string   `json:"rp_id"`
	Origin         string   `json:"origin"`
	AllowedCredIDs []string `json:"allow_credentials"`
}

// PasskeyView lists a user's passkeys.
type PasskeyView struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	CreatedAtMS  int64  `json:"created_at_ms"`
	LastUsedAtMS int64  `json:"last_used_at_ms,omitempty"`
}

// LoginView is one row of the recent-logins surface.
type LoginView struct {
	DeviceID   string `json:"device_id,omitempty"`
	IP         string `json:"ip"`
	UserAgent  string `json:"user_agent,omitempty"`
	AtMS       int64  `json:"at_ms"`
	Suspicious bool   `json:"suspicious"`
}

// Store persists passkeys, single-use challenges, and login events.
type Store interface {
	SaveChallenge(ctx context.Context, c Challenge) error
	TakeChallenge(ctx context.Context, value string) (Challenge, error) // fetch + delete (single-use); ErrNotFound

	CreateCredential(ctx context.Context, c Credential) error
	GetCredential(ctx context.Context, id string) (Credential, error) // ErrNotFound
	ListCredentials(ctx context.Context, userID string) ([]Credential, error)
	UpdateSignCount(ctx context.Context, id string, count uint32, usedAt time.Time) error
	DeleteCredential(ctx context.Context, userID, id string) error

	RecordLogin(ctx context.Context, e LoginEvent) error
	KnownIPs(ctx context.Context, userID string) ([]string, error)
	RecentLogins(ctx context.Context, userID string, limit int) ([]LoginEvent, error)
}
