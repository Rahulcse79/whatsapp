// Package profile is the self-profile + privacy + block-list context (FR-USER-01..03).
// The users row already carries username/display_name/about/privacy (migration
// 000002) and the blocks table exists (000004); this exposes them to clients.
// Content is never here — only identity metadata — so no E2EE concerns.
package profile

import (
	"context"
	"errors"
	"net/http"

	"github.com/whatsapp-v2/server/internal/platform/httpx"
)

// PrivacyKeys are the per-field visibility settings; each value is one of
// PrivacyValues. Absent = the app default (everyone).
var PrivacyKeys = []string{"last_seen", "avatar", "about", "read_receipts"}
var PrivacyValues = map[string]bool{"everyone": true, "contacts": true, "nobody": true}

// Profile is a user's editable identity metadata.
type Profile struct {
	UserID      string            `json:"user_id"`
	Username    string            `json:"username"`
	DisplayName string            `json:"display_name"`
	About       string            `json:"about"`
	Privacy     map[string]string `json:"privacy,omitempty"`
}

// ErrUsernameTaken is returned when a requested username is already in use.
var ErrUsernameTaken = errors.New("profile: username already taken")

// ErrNotFound is returned when a user id doesn't resolve to an active user.
var ErrNotFound = errors.New("profile: user not found")

// Store is the persistence port over the users + blocks tables.
type Store interface {
	Get(ctx context.Context, userID string) (Profile, error)                        // full self view
	Public(ctx context.Context, userID string) (Profile, error)                     // username/display/about only
	Update(ctx context.Context, userID, displayName, username, about string) error  // ErrUsernameTaken on conflict
	SetPrivacy(ctx context.Context, userID string, privacy map[string]string) error //
	Block(ctx context.Context, blocker, blocked string) error                       //
	Unblock(ctx context.Context, blocker, blocked string) error                     //
	Blocked(ctx context.Context, blocker string) ([]string, error)                  //
}

// Service applies the light validation the client can't be trusted to.
type Service struct{ store Store }

func NewService(store Store) *Service { return &Service{store: store} }

func (s *Service) Get(ctx context.Context, userID string) (Profile, error) {
	return s.store.Get(ctx, userID)
}

func (s *Service) Public(ctx context.Context, userID string) (Profile, error) {
	p, err := s.store.Public(ctx, userID)
	if errors.Is(err, ErrNotFound) {
		return Profile{}, httpx.Reject(http.StatusNotFound, "USER_NOT_FOUND", "no such user")
	}
	return p, err
}

// Update validates + writes display name / username / about. A username, when
// given, is normalised to lowercase and length-checked (3–30, [a-z0-9_.]).
func (s *Service) Update(ctx context.Context, userID, displayName, username, about string) error {
	if len(displayName) > 100 {
		return httpx.Reject(http.StatusBadRequest, "VALIDATION_NAME", "display name too long (max 100)")
	}
	if len(about) > 200 {
		return httpx.Reject(http.StatusBadRequest, "VALIDATION_ABOUT", "about too long (max 200)")
	}
	if username != "" && !validUsername(username) {
		return httpx.Reject(http.StatusBadRequest, "VALIDATION_USERNAME",
			"username must be 3–30 chars of a–z, 0–9, '_' or '.'")
	}
	err := s.store.Update(ctx, userID, displayName, username, about)
	if errors.Is(err, ErrUsernameTaken) {
		return httpx.Reject(http.StatusConflict, "USERNAME_TAKEN", "that username is already in use")
	}
	return err
}

func (s *Service) SetPrivacy(ctx context.Context, userID string, privacy map[string]string) error {
	clean := make(map[string]string, len(privacy))
	for _, k := range PrivacyKeys {
		if v, ok := privacy[k]; ok {
			if !PrivacyValues[v] {
				return httpx.Reject(http.StatusBadRequest, "VALIDATION_PRIVACY",
					"privacy value must be everyone|contacts|nobody")
			}
			clean[k] = v
		}
	}
	return s.store.SetPrivacy(ctx, userID, clean)
}

func (s *Service) Block(ctx context.Context, me, target string) error {
	if me == target {
		return httpx.Reject(http.StatusBadRequest, "VALIDATION_SELF", "cannot block yourself")
	}
	return s.store.Block(ctx, me, target)
}

func (s *Service) Unblock(ctx context.Context, me, target string) error {
	return s.store.Unblock(ctx, me, target)
}

func (s *Service) Blocked(ctx context.Context, me string) ([]string, error) {
	return s.store.Blocked(ctx, me)
}

func validUsername(u string) bool {
	if len(u) < 3 || len(u) > 30 {
		return false
	}
	for _, r := range u {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' && r != '.' {
			return false
		}
	}
	return true
}
