package payments

import (
	"context"

	"github.com/whatsapp-v2/server/internal/admin"
)

// AdminBridge adapts the payments service to the admin console's port, so the
// admin plane depends on a narrow interface rather than on this package's
// construction (and so neither package imports the other's internals).
type AdminBridge struct{ svc *Service }

func NewAdminBridge(svc *Service) *AdminBridge { return &AdminBridge{svc: svc} }

func (b *AdminBridge) AdminList(ctx context.Context, status string, limit int) ([]admin.PaymentRow, error) {
	ps, err := b.svc.AdminList(ctx, status, limit)
	if err != nil {
		return nil, err
	}
	out := make([]admin.PaymentRow, len(ps))
	for i, p := range ps {
		out[i] = admin.PaymentRow{
			ID: p.ID, Purpose: p.Purpose, AmountCents: p.AmountCents, Currency: p.Currency,
			Status: p.Status, Subject: p.Subject,
			CreatedAtMS: p.CreatedAtMS, UpdatedAtMS: p.UpdatedAtMS,
		}
	}
	return out, nil
}

func (b *AdminBridge) AdminRefund(ctx context.Context, paymentID string) error {
	return b.svc.AdminRefund(ctx, paymentID)
}

var _ admin.PaymentsBackend = (*AdminBridge)(nil)
