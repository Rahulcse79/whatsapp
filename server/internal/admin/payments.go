package admin

import (
	"context"
	"net/http"
	"strconv"

	"github.com/whatsapp-v2/server/internal/admin/domain"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
)

// The payments console (T15.05).
//
// Split from service.go because payments are a different blast radius from
// moderation: a refund moves real money, so it needs the strongest role in the
// model and an audit entry, whereas reading the ledger is a support task.
//
// This file deliberately holds no payment logic — it authorises, delegates to
// the payments service, and records what an operator did.

// PaymentsBackend is the slice of the payments service the console needs. An
// interface rather than the concrete service so the admin plane does not depend
// on the payments package's construction, and so the console is testable.
type PaymentsBackend interface {
	AdminList(ctx context.Context, status string, limit int) ([]PaymentRow, error)
	AdminRefund(ctx context.Context, paymentID string) error
}

// PaymentRow is one payment in the console. It carries no card data, because
// none exists anywhere in this system to carry.
type PaymentRow struct {
	ID          string `json:"id"`
	Purpose     string `json:"purpose"`
	AmountCents int64  `json:"amount_cents"`
	Currency    string `json:"currency"`
	Status      string `json:"status"`
	Subject     string `json:"subject,omitempty"`
	CreatedAtMS int64  `json:"created_at_ms"`
	UpdatedAtMS int64  `json:"updated_at_ms"`
}

// PaymentsConsole is the admin-facing payments surface.
type PaymentsConsole struct {
	backend PaymentsBackend
	audit   AuditWriter
}

// AuditWriter records an operator action. Refunds must be attributable.
//
// Unlike the rest of the admin plane this write is not co-transactional with
// its effect, because the effect is a reversal at the payment processor. See
// adapters.Store.AppendAudit for why, and what is done about it.
type AuditWriter interface {
	AppendAudit(ctx context.Context, e AuditEntry) error
}

func NewPaymentsConsole(backend PaymentsBackend, audit AuditWriter) *PaymentsConsole {
	return &PaymentsConsole{backend: backend, audit: audit}
}

// List returns payments for support. Agent+ — reading the ledger is routine
// support work, but not something a read-only viewer needs.
func (c *PaymentsConsole) List(ctx context.Context, admin Identity, status string, limit int) ([]PaymentRow, error) {
	if err := require(admin, domain.RoleAgent); err != nil {
		return nil, err
	}
	return c.backend.AdminList(ctx, status, limit)
}

// Refund reverses a payment. Owner-only: it moves money, which is the highest-
// consequence action in the console, and it is always audited — including the
// operator's stated reason, so a refund can be explained after the fact.
func (c *PaymentsConsole) Refund(ctx context.Context, admin Identity, paymentID, reason string) error {
	if err := require(admin, domain.RoleOwner); err != nil {
		return err
	}
	if reason == "" {
		return httpx.Reject(http.StatusBadRequest, "VALIDATION_REASON", "a refund needs a reason for the audit trail")
	}
	if err := c.backend.AdminRefund(ctx, paymentID); err != nil {
		return err
	}
	// Audit after the money moves: recording a refund that did not happen is
	// worse than a missing entry, and a write failure here must not imply the
	// refund was reversed.
	if err := c.audit.AppendAudit(ctx, AuditEntry{
		Actor: admin.Subject, Action: "payment.refund", Target: paymentID, Reason: reason,
	}); err != nil {
		return err
	}
	return nil
}

// PaymentsRoutes mounts the console surface.
func PaymentsRoutes(mux *http.ServeMux, s *Service, c *PaymentsConsole) {
	mux.HandleFunc("GET /admin/v1/payments", func(w http.ResponseWriter, r *http.Request) {
		admin, ok := authenticate(w, r, s)
		if !ok {
			return
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		rows, err := c.List(r.Context(), admin, r.URL.Query().Get("status"), limit)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"payments": rows})
	})

	mux.HandleFunc("POST /admin/v1/payments/{id}/refund", func(w http.ResponseWriter, r *http.Request) {
		admin, ok := authenticate(w, r, s)
		if !ok {
			return
		}
		var body struct {
			Reason string `json:"reason"`
		}
		if !decode(w, r, &body) {
			return
		}
		if err := c.Refund(r.Context(), admin, r.PathValue("id"), body.Reason); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}
