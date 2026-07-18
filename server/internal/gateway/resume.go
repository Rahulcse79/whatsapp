package gateway

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// resumeTokenTTL keeps a resume token useful for as long as the inbox holds
// undelivered ciphertext (30 d backstop, ADR-001). The token only skips the
// full-replay path — the JWT is still verified on every connect, so a long
// TTL costs nothing in auth strength.
const resumeTokenTTL = 30 * 24 * time.Hour

// ResumeStore issues and checks per-device resume tokens
// (ws-gateway-lld §3: resume:{device} holds a hash, rotated every connect).
type ResumeStore interface {
	// Rotate issues a fresh token for the device, invalidating any prior one
	// (exactly one active resume token per device).
	Rotate(ctx context.Context, deviceID string, ttl time.Duration) (token string, err error)
	// Validate reports whether token is the device's current resume token.
	Validate(ctx context.Context, deviceID, token string) (bool, error)
}

// NewResumeToken returns a fresh random token (256-bit, URL-safe).
func NewResumeToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generating resume token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

// HashResumeToken is the storable form: the store never holds the token
// itself, so a leaked Valkey snapshot cannot resume sessions.
func HashResumeToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// MemoryResumeStore is an in-process ResumeStore for tests and single-pod
// dev. Production uses the Valkey store (adapters/resume.go). TTLs are not
// enforced here — tests exercise rotation and validation, not expiry.
type MemoryResumeStore struct {
	mu     sync.Mutex
	hashes map[string]string
}

func NewMemoryResumeStore() *MemoryResumeStore {
	return &MemoryResumeStore{hashes: make(map[string]string)}
}

func (m *MemoryResumeStore) Rotate(_ context.Context, deviceID string, _ time.Duration) (string, error) {
	token, err := NewResumeToken()
	if err != nil {
		return "", err
	}
	m.mu.Lock()
	m.hashes[deviceID] = HashResumeToken(token)
	m.mu.Unlock()
	return token, nil
}

func (m *MemoryResumeStore) Validate(_ context.Context, deviceID, token string) (bool, error) {
	m.mu.Lock()
	stored, ok := m.hashes[deviceID]
	m.mu.Unlock()
	if !ok {
		return false, nil
	}
	return subtle.ConstantTimeCompare([]byte(stored), []byte(HashResumeToken(token))) == 1, nil
}
