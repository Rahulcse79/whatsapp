// Package payments is the monetization backbone (T15.05): premium
// subscriptions, paid-channel subscriptions, and the person-to-person transfer
// seam.
//
// THE RULE THIS PACKAGE EXISTS TO ENFORCE: **no raw card data, ever.** We
// integrate with a payment service provider in its hosted/tokenised mode — the
// payer enters their details on the PSP's own surface (a redirect or the PSP's
// SDK), and this system only ever stores opaque processor references, amounts
// and statuses. That keeps the deployment in PCI-DSS SAQ-A and means a database
// or log leak here cannot expose anyone's card.
//
// Consequences that are deliberate, not oversights:
//   - There is no endpoint that accepts a card number, and the domain's
//     card-data guard rejects anything PAN-shaped in free-text fields.
//   - We never "charge" synchronously. We create an intent with the PSP and
//     learn the outcome from a *signed* webhook, because the payer completes
//     payment out of band.
//   - P2P transfer is a SEAM, not a money-transmitter implementation. Moving
//     money between users is a regulated activity; the port lets a licensed
//     provider be plugged in, and the default build refuses.
package payments

import (
	"context"
	"errors"
	"time"

	"github.com/whatsapp-v2/server/internal/payments/domain"
)

var (
	ErrNotFound     = errors.New("payments: not found")
	ErrPSPDisabled  = errors.New("payments: no payment provider configured")
	ErrP2PDisabled  = errors.New("payments: person-to-person transfers are not enabled on this deployment")
	ErrDuplicateEvt = errors.New("payments: webhook event already processed")
)

// Payment is one attempt to move money, in our records. It holds no card data —
// only what the PSP told us to remember.
type Payment struct {
	ID        string
	UserID    string // the payer
	Purpose   domain.Purpose
	Amount    domain.Money
	Status    domain.Status
	PSPRef    string // the processor's id for this intent/charge — opaque to us
	Subject   string // what is being paid for: channel id, payee user id, or ""
	Memo      string // user-supplied note (card-data guarded)
	CreatedAt time.Time
	UpdatedAt time.Time
}

// PaymentView is a payment over the wire.
type PaymentView struct {
	ID          string `json:"id"`
	Purpose     string `json:"purpose"`
	AmountCents int64  `json:"amount_cents"`
	Currency    string `json:"currency"`
	Status      string `json:"status"`
	Subject     string `json:"subject,omitempty"`
	Memo        string `json:"memo,omitempty"`
	CreatedAtMS int64  `json:"created_at_ms"`
	UpdatedAtMS int64  `json:"updated_at_ms"`
}

// Subscription is a recurring entitlement bought by a payment.
type Subscription struct {
	ID         string
	UserID     string
	Purpose    domain.Purpose // premium | channel_sub
	Subject    string         // channel id for channel_sub, "" for premium
	PSPRef     string
	Active     bool
	StartedAt  time.Time
	ExpiresAt  time.Time
	CanceledAt *time.Time
}

// SubscriptionView is a subscription over the wire.
type SubscriptionView struct {
	ID          string `json:"id"`
	Purpose     string `json:"purpose"`
	Subject     string `json:"subject,omitempty"`
	Active      bool   `json:"active"`
	StartedAtMS int64  `json:"started_at_ms"`
	ExpiresAtMS int64  `json:"expires_at_ms"`
	Canceled    bool   `json:"canceled,omitempty"`
}

// Checkout is what a client needs to finish paying: a redirect to the PSP's
// hosted surface. We never render a card form ourselves.
type Checkout struct {
	PaymentID   string `json:"payment_id"`
	RedirectURL string `json:"redirect_url"`
	PSPRef      string `json:"psp_ref"`
	ExpiresAtMS int64  `json:"expires_at_ms"`
}

// Intent is what the service asks the PSP to create.
type Intent struct {
	PaymentID string
	UserID    string
	Purpose   domain.Purpose
	Amount    domain.Money
	Subject   string
	// Recurring asks for a subscription rather than a one-off charge.
	Recurring bool
}

// Event is a normalised PSP webhook, after signature verification.
type Event struct {
	// ID is the PSP's event id — the idempotency key. A PSP may deliver the
	// same event many times; we record ids and ignore repeats.
	ID     string
	PSPRef string        // which intent/charge it concerns
	Status domain.Status // the new status
	// RawKind is the provider's own event name, kept for the audit trail.
	RawKind string
}

// PSP is the payment-provider port. Implementations talk to a real processor;
// the default build wires a disabled stub so a deployment cannot accidentally
// take money without an explicit choice.
//
// Note what is NOT here: nothing accepts a card number, expiry, or CVV. The
// only thing that crosses this boundary is an amount, a reference, and a URL.
type PSP interface {
	// CreateCheckout registers an intent with the processor and returns the
	// hosted URL to send the payer to.
	CreateCheckout(ctx context.Context, in Intent) (Checkout, error)
	// Refund reverses a settled payment by its processor reference.
	Refund(ctx context.Context, pspRef string) error
	// VerifyWebhook authenticates a raw callback body and normalises it.
	// It MUST be given the raw bytes — re-serialising breaks the signature.
	VerifyWebhook(payload []byte, signature string) (Event, error)
	// Name identifies the provider in logs and the admin console.
	Name() string
	// Enabled reports whether this deployment can actually take payments.
	Enabled() bool
}

// Transfers is the person-to-person money-movement seam, split from PSP because
// it is a different regulatory animal: sending money between users generally
// requires a money-transmitter licence. The default build returns
// ErrP2PDisabled; a deployment with a licensed provider injects an adapter.
type Transfers interface {
	Send(ctx context.Context, fromUserID, toUserID string, amount domain.Money, memo string) (pspRef string, err error)
	Enabled() bool
}

// Store persists payments, subscriptions, and processed webhook ids.
type Store interface {
	CreatePayment(ctx context.Context, p Payment) error
	GetPayment(ctx context.Context, id string) (Payment, error)          // ErrNotFound
	GetPaymentByPSPRef(ctx context.Context, ref string) (Payment, error) // ErrNotFound
	ListPaymentsByUser(ctx context.Context, userID string, limit int) ([]Payment, error)
	// UpdateStatus moves a payment and, when the new status grants access,
	// upserts the subscription in the SAME transaction — an entitlement must
	// never disagree with the payment that bought it.
	UpdateStatus(ctx context.Context, paymentID string, to domain.Status, at time.Time, sub *Subscription) error

	ListSubscriptionsByUser(ctx context.Context, userID string) ([]Subscription, error)
	ActiveSubscription(ctx context.Context, userID string, purpose domain.Purpose, subject string, now time.Time) (Subscription, error) // ErrNotFound
	CancelSubscription(ctx context.Context, userID, subscriptionID string, at time.Time) error

	// MarkEventProcessed records a PSP event id, returning ErrDuplicateEvt if
	// it was already seen. This is the webhook idempotency gate.
	MarkEventProcessed(ctx context.Context, eventID, pspRef, rawKind string, at time.Time) error

	// Admin surfaces.
	ListPayments(ctx context.Context, status string, limit int) ([]Payment, error)
}
