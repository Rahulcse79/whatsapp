package adapters

import (
	"context"

	"github.com/whatsapp-v2/server/internal/platform/id"
)

// NoopGateway is the default channels.PaymentGateway for dev + self-hosted
// deployments: it records a placeholder reference and NEVER moves money,
// honouring the project's no-payments stance. A hosted deployment swaps in a
// real processor adapter (Stripe/…) that implements the same Charge contract.
type NoopGateway struct{}

func NewNoopGateway() *NoopGateway { return &NoopGateway{} }

// Charge always "succeeds" with a synthetic reference. The cents amount is
// ignored — no charge is made.
func (NoopGateway) Charge(_ context.Context, _, _ string, _ int) (string, error) {
	return "noop-" + id.New(), nil
}
