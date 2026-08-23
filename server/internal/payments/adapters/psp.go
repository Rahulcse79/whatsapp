package adapters

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/whatsapp-v2/server/internal/payments"
	"github.com/whatsapp-v2/server/internal/payments/domain"
)

// ── The default: no provider ───────────────────────────────────────────────

// DisabledPSP is what a deployment gets until it deliberately configures a
// processor. It refuses rather than pretending, because the alternative — a
// no-op that silently "succeeds" — would grant paid entitlements for free and
// look like a working payment system in testing.
type DisabledPSP struct{}

func NewDisabledPSP() DisabledPSP { return DisabledPSP{} }

func (DisabledPSP) CreateCheckout(context.Context, payments.Intent) (payments.Checkout, error) {
	return payments.Checkout{}, payments.ErrPSPDisabled
}
func (DisabledPSP) Refund(context.Context, string) error { return payments.ErrPSPDisabled }
func (DisabledPSP) VerifyWebhook([]byte, string) (payments.Event, error) {
	return payments.Event{}, payments.ErrPSPDisabled
}
func (DisabledPSP) Name() string  { return "disabled" }
func (DisabledPSP) Enabled() bool { return false }

// ── A hosted-checkout provider ─────────────────────────────────────────────

// HostedPSP integrates a processor in its hosted-checkout mode: we register an
// intent, the payer completes payment on the processor's own page, and the
// outcome arrives as a signed webhook.
//
// This is the shape every major processor supports (Stripe Checkout, Adyen
// hosted, Razorpay, Paddle, Mollie…). Mapping a specific one means implementing
// `registerIntent` against its API and `mapKind` against its event names —
// nothing else changes, and in particular no card data enters this process.
type HostedPSP struct {
	name          string
	checkoutBase  string
	webhookSecret []byte
	// registerIntent is the provider call. Injected so the adapter is testable
	// without a network and so a deployment can slot in its processor's SDK.
	registerIntent func(ctx context.Context, in payments.Intent) (redirectURL, pspRef string, err error)
	// refund is the provider's reversal call.
	refund func(ctx context.Context, pspRef string) error
	// checkoutTTL bounds how long a hosted page stays valid.
	checkoutTTL time.Duration
	now         func() time.Time
}

// HostedConfig configures a hosted-checkout provider.
type HostedConfig struct {
	Name          string
	CheckoutBase  string // the processor's hosted-page base URL
	WebhookSecret string
	// RegisterIntent and Refund are the two provider calls. When nil, the
	// adapter falls back to deriving a deterministic reference from the payment
	// id and a redirect under CheckoutBase — usable for a staging environment
	// wired to a processor's test mode via its own dashboard, never for real
	// money.
	RegisterIntent func(ctx context.Context, in payments.Intent) (redirectURL, pspRef string, err error)
	Refund         func(ctx context.Context, pspRef string) error
	CheckoutTTL    time.Duration
}

var ErrHostedNotConfigured = errors.New("payments: hosted PSP needs a name, checkout base URL and webhook secret")

func NewHostedPSP(cfg HostedConfig) (*HostedPSP, error) {
	if strings.TrimSpace(cfg.Name) == "" || strings.TrimSpace(cfg.CheckoutBase) == "" || strings.TrimSpace(cfg.WebhookSecret) == "" {
		return nil, ErrHostedNotConfigured
	}
	ttl := cfg.CheckoutTTL
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}
	p := &HostedPSP{
		name: cfg.Name, checkoutBase: strings.TrimRight(cfg.CheckoutBase, "/"),
		webhookSecret:  []byte(cfg.WebhookSecret),
		registerIntent: cfg.RegisterIntent, refund: cfg.Refund,
		checkoutTTL: ttl, now: time.Now,
	}
	return p, nil
}

func (p *HostedPSP) CreateCheckout(ctx context.Context, in payments.Intent) (payments.Checkout, error) {
	var (
		url, ref string
		err      error
	)
	if p.registerIntent != nil {
		url, ref, err = p.registerIntent(ctx, in)
		if err != nil {
			return payments.Checkout{}, err
		}
	} else {
		// No provider call wired: address the hosted page by our own payment id.
		// The processor must be configured to echo it back as the reference.
		ref = in.PaymentID
		url = p.checkoutBase + "/" + in.PaymentID
	}
	return payments.Checkout{
		RedirectURL: url,
		PSPRef:      ref,
		ExpiresAtMS: p.now().Add(p.checkoutTTL).UnixMilli(),
	}, nil
}

func (p *HostedPSP) Refund(ctx context.Context, pspRef string) error {
	if p.refund == nil {
		return errors.New("payments: this provider has no refund call wired")
	}
	return p.refund(ctx, pspRef)
}

// hostedEvent is the normalised webhook body. A real provider's payload is
// mapped onto this shape by the deployment's adapter; the signature check and
// the status mapping are what matter here.
type hostedEvent struct {
	ID     string `json:"id"`
	Ref    string `json:"psp_ref"`
	Kind   string `json:"type"`
	Status string `json:"status"`
}

// VerifyWebhook authenticates the RAW body then maps it. The raw bytes are
// non-negotiable: re-encoding JSON reorders keys and the signature would never
// match, and "fixing" that by skipping verification is how forged payment
// events get accepted.
func (p *HostedPSP) VerifyWebhook(payload []byte, signature string) (payments.Event, error) {
	if err := domain.VerifyWebhook(p.webhookSecret, payload, signature); err != nil {
		return payments.Event{}, err
	}
	var ev hostedEvent
	if err := json.Unmarshal(payload, &ev); err != nil {
		return payments.Event{}, err
	}
	if ev.ID == "" || ev.Ref == "" {
		return payments.Event{}, errors.New("payments: webhook missing id or reference")
	}
	status, err := mapKind(ev.Kind, ev.Status)
	if err != nil {
		return payments.Event{}, err
	}
	return payments.Event{ID: ev.ID, PSPRef: ev.Ref, Status: status, RawKind: ev.Kind}, nil
}

// mapKind translates a provider event onto our status. An unknown event is an
// error, not a guess: silently treating an unrecognised event as "succeeded"
// would be a way to grant entitlements for free.
func mapKind(kind, status string) (domain.Status, error) {
	switch {
	case strings.Contains(kind, "succeed"), strings.Contains(kind, "captur"), strings.Contains(kind, "paid"):
		return domain.StatusSucceeded, nil
	case strings.Contains(kind, "fail"), strings.Contains(kind, "declin"):
		return domain.StatusFailed, nil
	case strings.Contains(kind, "cancel"), strings.Contains(kind, "expir"):
		return domain.StatusCanceled, nil
	case strings.Contains(kind, "refund"):
		return domain.StatusRefunded, nil
	}
	// Fall back to an explicit status field if the provider sends one.
	if status != "" {
		return domain.ParseStatus(status)
	}
	return "", errors.New("payments: unrecognised provider event " + kind)
}

func (p *HostedPSP) Name() string  { return p.name }
func (p *HostedPSP) Enabled() bool { return true }

// ── P2P ────────────────────────────────────────────────────────────────────

// DisabledTransfers is the default person-to-person seam. Moving money between
// users is generally a licensed activity (money transmission), so the shipped
// build refuses and a deployment must inject a provider that holds the licence.
type DisabledTransfers struct{}

func NewDisabledTransfers() DisabledTransfers { return DisabledTransfers{} }

func (DisabledTransfers) Send(context.Context, string, string, domain.Money, string) (string, error) {
	return "", payments.ErrP2PDisabled
}
func (DisabledTransfers) Enabled() bool { return false }

var (
	_ payments.PSP       = DisabledPSP{}
	_ payments.PSP       = (*HostedPSP)(nil)
	_ payments.Transfers = DisabledTransfers{}
)
