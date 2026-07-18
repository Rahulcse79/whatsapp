package adapters

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/whatsapp-v2/server/internal/gateway"
)

// ValkeyResumeStore implements gateway.ResumeStore. Only the SHA-256 of the
// token is stored (resume:{device}), so the store never holds a usable
// credential.
type ValkeyResumeStore struct{ client *redis.Client }

func NewValkeyResumeStore(client *redis.Client) *ValkeyResumeStore {
	return &ValkeyResumeStore{client: client}
}

func resumeKey(deviceID string) string { return "resume:" + deviceID }

// Rotate issues a fresh token, unconditionally replacing the previous one:
// the newest connection owns the device's resume lineage (same last-writer-
// wins rule as routes).
func (s *ValkeyResumeStore) Rotate(ctx context.Context, deviceID string, ttl time.Duration) (string, error) {
	token, err := gateway.NewResumeToken()
	if err != nil {
		return "", err
	}
	if err := s.client.Set(ctx, resumeKey(deviceID), gateway.HashResumeToken(token), ttl).Err(); err != nil {
		return "", fmt.Errorf("storing resume token: %w", err)
	}
	return token, nil
}

// Validate reports whether token matches the device's current stored hash.
// An absent key (expired / never issued) is a clean false, not an error.
func (s *ValkeyResumeStore) Validate(ctx context.Context, deviceID, token string) (bool, error) {
	stored, err := s.client.Get(ctx, resumeKey(deviceID)).Result()
	if errors.Is(err, redis.Nil) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("loading resume token: %w", err)
	}
	return subtle.ConstantTimeCompare([]byte(stored), []byte(gateway.HashResumeToken(token))) == 1, nil
}

var _ gateway.ResumeStore = (*ValkeyResumeStore)(nil)
