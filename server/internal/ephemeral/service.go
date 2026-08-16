package ephemeral

import (
	"context"
	"net/http"
	"time"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/ephemeral/domain"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
)

// Service is the disappearing-timer control plane: get/set the per-conversation
// timer (membership-gated) and the backstop purge.
type Service struct {
	store Store
	now   func() time.Time
}

func NewService(store Store) *Service { return &Service{store: store, now: time.Now} }

// GetTimer returns the conversation's disappearing timer (members only).
func (s *Service) GetTimer(ctx context.Context, ident auth.Identity, conversationID string) (int, error) {
	if err := s.requireMember(ctx, conversationID, ident.UserID); err != nil {
		return 0, err
	}
	ttl, err := s.store.GetTimer(ctx, domain.NormalizeConversationID(conversationID))
	if err != nil {
		return 0, httpx.Transient()
	}
	return ttl, nil
}

// SetTimer sets the conversation's disappearing timer (members only). The value
// also rides an E2EE control message between clients; this copy drives the purge
// backstop and lets a fresh device learn the setting.
func (s *Service) SetTimer(ctx context.Context, ident auth.Identity, conversationID string, ttlSeconds int) error {
	if err := domain.ValidateTTL(ttlSeconds); err != nil {
		return httpx.Reject(http.StatusBadRequest, "VALIDATION_TTL", err.Error())
	}
	if err := s.requireMember(ctx, conversationID, ident.UserID); err != nil {
		return err
	}
	if err := s.store.SetTimer(ctx, domain.NormalizeConversationID(conversationID), ttlSeconds, ident.UserID, s.now()); err != nil {
		return httpx.Transient()
	}
	return nil
}

// PurgeExpired runs the backstop sweep (wired to a ticker in core-api).
func (s *Service) PurgeExpired(ctx context.Context) (int, error) {
	return s.store.PurgeExpired(ctx, s.now())
}

func (s *Service) requireMember(ctx context.Context, conversationID, userID string) error {
	ok, err := s.store.IsMember(ctx, domain.NormalizeConversationID(conversationID), userID)
	if err != nil {
		return httpx.Transient()
	}
	if !ok {
		// Don't reveal the conversation to non-members probing its timer.
		return httpx.Reject(http.StatusNotFound, "CONVERSATION_NOT_FOUND", "conversation not found")
	}
	return nil
}
