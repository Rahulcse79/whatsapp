package ai

import (
	"context"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/platform/flags"
)

// Service resolves the client AI runtime configuration (kill-switch + endpoint
// availability).
type Service struct {
	flags Flags
}

func NewService(f Flags) *Service { return &Service{flags: f} }

// Config returns the caller's AI configuration. Enabled honours the AI
// kill-switch (fail-open: AI is on unless an operator trips it).
func (s *Service) Config(ctx context.Context, ident auth.Identity) Config {
	return Config{
		Enabled:                 s.flags.Allowed(ctx, flags.KillAI, ident.UserID),
		ServerEndpointAvailable: false, // on-device only until a disclosed endpoint is provisioned
	}
}
