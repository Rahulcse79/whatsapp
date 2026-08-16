// Package ai is the minimal server surface for the on-device AI runtime (T11.01):
// it exposes the AI kill-switch to clients so an operator can disable all AI
// features org-wide, and advertises whether an opt-in server-inference endpoint
// is available (none by default — AI runs on-device, and the server never sees
// E2EE content unless the user explicitly opts in and discloses). The runtime,
// consent, and disclosure UX live on the client (@wa/client-core + web).
package ai

import (
	"context"

	"github.com/whatsapp-v2/server/internal/platform/flags"
)

// Flags is the kill-switch port (flags.Service in prod).
type Flags interface {
	Allowed(ctx context.Context, sw flags.KillSwitch, subject string) bool
}

// Config is GET /v1/ai/config — the client gates its AI runtime on this.
type Config struct {
	// Enabled is false when an operator has tripped the AI kill-switch.
	Enabled bool `json:"enabled"`
	// ServerEndpointAvailable advertises an opt-in, explicitly-disclosed server
	// inference endpoint. False here — AI is on-device only until such an endpoint
	// is provisioned (a documented seam); the client defaults to on-device.
	ServerEndpointAvailable bool `json:"server_endpoint_available"`
}
