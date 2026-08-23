package adapters

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/whatsapp-v2/server/internal/payments"
	"github.com/whatsapp-v2/server/internal/payments/domain"
)

// The shipped default must refuse everything: a no-op that "succeeds" would
// hand out paid entitlements for free and still look healthy in testing.
func TestDisabledPSPRefusesEverything(t *testing.T) {
	p := NewDisabledPSP()
	if p.Enabled() {
		t.Fatal("the default provider must report itself disabled")
	}
	if _, err := p.CreateCheckout(context.Background(), payments.Intent{}); err != payments.ErrPSPDisabled {
		t.Error("checkout must refuse")
	}
	if err := p.Refund(context.Background(), "ref"); err != payments.ErrPSPDisabled {
		t.Error("refund must refuse")
	}
	if _, err := p.VerifyWebhook([]byte("{}"), "sig"); err != payments.ErrPSPDisabled {
		t.Error("webhooks must refuse")
	}
}

func TestDisabledTransfersRefuses(t *testing.T) {
	tr := NewDisabledTransfers()
	if tr.Enabled() {
		t.Fatal("p2p must be off by default — it is a licensed activity")
	}
	if _, err := tr.Send(context.Background(), "a", "b", domain.Money{Cents: 1, Currency: "USD"}, ""); err != payments.ErrP2PDisabled {
		t.Error("send must refuse")
	}
}

func TestHostedPSPRequiresFullConfig(t *testing.T) {
	for _, c := range []HostedConfig{
		{Name: "", CheckoutBase: "https://p.test", WebhookSecret: "s"},
		{Name: "p", CheckoutBase: "", WebhookSecret: "s"},
		{Name: "p", CheckoutBase: "https://p.test", WebhookSecret: ""},
	} {
		if _, err := NewHostedPSP(c); err != ErrHostedNotConfigured {
			t.Errorf("%+v should be rejected, got %v", c, err)
		}
	}
}

func hosted(t *testing.T) *HostedPSP {
	t.Helper()
	p, err := NewHostedPSP(HostedConfig{Name: "test-psp", CheckoutBase: "https://psp.test/pay/", WebhookSecret: "whsec"})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestHostedCheckout(t *testing.T) {
	p := hosted(t)
	co, err := p.CreateCheckout(context.Background(), payments.Intent{PaymentID: "pay1"})
	if err != nil {
		t.Fatal(err)
	}
	if co.RedirectURL != "https://psp.test/pay/pay1" {
		t.Fatalf("unexpected redirect: %s", co.RedirectURL)
	}
	if co.ExpiresAtMS == 0 {
		t.Error("a hosted page should carry an expiry")
	}
}

func signed(t *testing.T, p *HostedPSP, ev hostedEvent) ([]byte, string) {
	t.Helper()
	body, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	return body, domain.SignWebhook([]byte("whsec"), body)
}

func TestHostedWebhookVerifiesAndMaps(t *testing.T) {
	p := hosted(t)
	cases := []struct {
		kind string
		want domain.Status
	}{
		{"payment.succeeded", domain.StatusSucceeded},
		{"charge.captured", domain.StatusSucceeded},
		{"invoice.paid", domain.StatusSucceeded},
		{"payment.failed", domain.StatusFailed},
		{"card.declined", domain.StatusFailed},
		{"checkout.canceled", domain.StatusCanceled},
		{"checkout.expired", domain.StatusCanceled},
		{"charge.refunded", domain.StatusRefunded},
	}
	for _, c := range cases {
		body, sig := signed(t, p, hostedEvent{ID: "e1", Ref: "r1", Kind: c.kind})
		ev, err := p.VerifyWebhook(body, sig)
		if err != nil {
			t.Errorf("%s: %v", c.kind, err)
			continue
		}
		if ev.Status != c.want {
			t.Errorf("%s mapped to %s, want %s", c.kind, ev.Status, c.want)
		}
		if ev.RawKind != c.kind {
			t.Errorf("the provider's own event name should be preserved for the audit trail")
		}
	}
}

func TestHostedWebhookRejectsForgeryAndTampering(t *testing.T) {
	p := hosted(t)
	body, sig := signed(t, p, hostedEvent{ID: "e1", Ref: "r1", Kind: "payment.succeeded"})

	if _, err := p.VerifyWebhook(body, "deadbeef"); err != domain.ErrBadSignature {
		t.Error("a bogus signature must be rejected")
	}
	tampered := []byte(string(body) + " ")
	if _, err := p.VerifyWebhook(tampered, sig); err != domain.ErrBadSignature {
		t.Error("altering the body by even one byte must invalidate the signature")
	}
	if _, err := p.VerifyWebhook(body, ""); err != domain.ErrBadSignature {
		t.Error("a missing signature must never be treated as acceptable")
	}
}

// An event we do not recognise must be an error, never an optimistic
// "succeeded" — otherwise an unknown provider event grants free access.
func TestHostedWebhookRejectsUnknownEvent(t *testing.T) {
	p := hosted(t)
	body, sig := signed(t, p, hostedEvent{ID: "e1", Ref: "r1", Kind: "customer.updated"})
	if _, err := p.VerifyWebhook(body, sig); err == nil {
		t.Fatal("an unrecognised event must not be silently mapped")
	}
}

func TestHostedWebhookRequiresIDAndRef(t *testing.T) {
	p := hosted(t)
	for _, ev := range []hostedEvent{
		{ID: "", Ref: "r1", Kind: "payment.succeeded"},
		{ID: "e1", Ref: "", Kind: "payment.succeeded"},
	} {
		body, sig := signed(t, p, ev)
		if _, err := p.VerifyWebhook(body, sig); err == nil {
			t.Errorf("%+v should be rejected: without an id there is no idempotency key, without a ref no payment to apply it to", ev)
		}
	}
}
