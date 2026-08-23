package payments

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/payments/domain"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
)

// ── fakes ───────────────────────────────────────────────────────────────────

type fakeStore struct {
	payments map[string]Payment
	byRef    map[string]string // pspRef → paymentID
	subs     map[string]Subscription
	events   map[string]bool
	failNext error
	// updates counts UpdateStatus calls so a test can prove a duplicate webhook
	// ran no side effects.
	updates int
}

func newStore() *fakeStore {
	return &fakeStore{
		payments: map[string]Payment{}, byRef: map[string]string{},
		subs: map[string]Subscription{}, events: map[string]bool{},
	}
}

func (s *fakeStore) CreatePayment(_ context.Context, p Payment) error {
	if s.failNext != nil {
		err := s.failNext
		s.failNext = nil
		return err
	}
	s.payments[p.ID] = p
	if p.PSPRef != "" {
		s.byRef[p.PSPRef] = p.ID
	}
	return nil
}
func (s *fakeStore) GetPayment(_ context.Context, id string) (Payment, error) {
	p, ok := s.payments[id]
	if !ok {
		return Payment{}, ErrNotFound
	}
	return p, nil
}
func (s *fakeStore) GetPaymentByPSPRef(_ context.Context, ref string) (Payment, error) {
	id, ok := s.byRef[ref]
	if !ok {
		return Payment{}, ErrNotFound
	}
	return s.payments[id], nil
}
func (s *fakeStore) ListPaymentsByUser(_ context.Context, userID string, _ int) ([]Payment, error) {
	var out []Payment
	for _, p := range s.payments {
		if p.UserID == userID {
			out = append(out, p)
		}
	}
	return out, nil
}
func (s *fakeStore) UpdateStatus(_ context.Context, id string, to domain.Status, at time.Time, sub *Subscription) error {
	s.updates++
	p := s.payments[id]
	p.Status, p.UpdatedAt = to, at
	s.payments[id] = p
	if sub != nil {
		s.subs[sub.ID] = *sub
	}
	return nil
}
func (s *fakeStore) ListSubscriptionsByUser(_ context.Context, userID string) ([]Subscription, error) {
	var out []Subscription
	for _, sb := range s.subs {
		if sb.UserID == userID {
			out = append(out, sb)
		}
	}
	return out, nil
}
func (s *fakeStore) ActiveSubscription(_ context.Context, userID string, purpose domain.Purpose, subject string, now time.Time) (Subscription, error) {
	for _, sb := range s.subs {
		if sb.UserID == userID && sb.Purpose == purpose && sb.Subject == subject && sb.Active && sb.ExpiresAt.After(now) {
			return sb, nil
		}
	}
	return Subscription{}, ErrNotFound
}
func (s *fakeStore) CancelSubscription(_ context.Context, userID, id string, at time.Time) error {
	sb, ok := s.subs[id]
	if !ok || sb.UserID != userID {
		return nil
	}
	sb.CanceledAt = &at
	s.subs[id] = sb
	return nil
}
func (s *fakeStore) MarkEventProcessed(_ context.Context, eventID, _, _ string, _ time.Time) error {
	if s.events[eventID] {
		return ErrDuplicateEvt
	}
	s.events[eventID] = true
	return nil
}
func (s *fakeStore) ListPayments(_ context.Context, status string, _ int) ([]Payment, error) {
	var out []Payment
	for _, p := range s.payments {
		if status == "" || string(p.Status) == status {
			out = append(out, p)
		}
	}
	return out, nil
}

// fakePSP mints predictable refs and signs webhooks with a known secret.
type fakePSP struct {
	secret   []byte
	enabled  bool
	failNext error
	refunds  []string
	n        int
}

func (p *fakePSP) CreateCheckout(_ context.Context, in Intent) (Checkout, error) {
	if p.failNext != nil {
		err := p.failNext
		p.failNext = nil
		return Checkout{}, err
	}
	p.n++
	return Checkout{RedirectURL: fmt.Sprintf("https://psp.test/c/%d", p.n), PSPRef: fmt.Sprintf("ref%d", p.n)}, nil
}
func (p *fakePSP) Refund(_ context.Context, ref string) error {
	if p.failNext != nil {
		err := p.failNext
		p.failNext = nil
		return err
	}
	p.refunds = append(p.refunds, ref)
	return nil
}
func (p *fakePSP) VerifyWebhook(payload []byte, sig string) (Event, error) {
	if err := domain.VerifyWebhook(p.secret, payload, sig); err != nil {
		return Event{}, err
	}
	// Body format for the fake: "eventID|pspRef|status|kind".
	var id, ref, st, kind string
	if _, err := fmt.Sscanf(string(payload), "%s", &id); err != nil {
		return Event{}, err
	}
	parts := splitN(string(payload), '|', 4)
	if len(parts) != 4 {
		return Event{}, errors.New("bad body")
	}
	id, ref, st, kind = parts[0], parts[1], parts[2], parts[3]
	status, err := domain.ParseStatus(st)
	if err != nil {
		return Event{}, err
	}
	return Event{ID: id, PSPRef: ref, Status: status, RawKind: kind}, nil
}
func (p *fakePSP) Name() string  { return "fake" }
func (p *fakePSP) Enabled() bool { return p.enabled }

func splitN(s string, sep byte, n int) []string {
	out := make([]string, 0, n)
	cur := ""
	for i := 0; i < len(s); i++ {
		if s[i] == sep && len(out) < n-1 {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(s[i])
	}
	return append(out, cur)
}

type fakeTransfers struct{ enabled bool }

func (t fakeTransfers) Send(context.Context, string, string, domain.Money, string) (string, error) {
	return "xfer1", nil
}
func (t fakeTransfers) Enabled() bool { return t.enabled }

// ── helpers ─────────────────────────────────────────────────────────────────

func who(u string) auth.Identity { return auth.Identity{UserID: u} }

func codeOf(t *testing.T, err error) string {
	t.Helper()
	var ae *httpx.APIError
	if !errors.As(err, &ae) {
		t.Fatalf("want *httpx.APIError, got %T: %v", err, err)
	}
	return ae.Code
}

func newSvc(enabled bool) (*Service, *fakeStore, *fakePSP) {
	st, psp := newStore(), &fakePSP{secret: []byte("psp-secret"), enabled: enabled}
	svc := NewService(st, psp, fakeTransfers{enabled: false}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	n := 0
	svc.newID = func() string { n++; return fmt.Sprintf("id%d", n) }
	svc.now = func() time.Time { return time.UnixMilli(1_000_000) }
	return svc, st, psp
}

// hook builds a signed webhook body for the fake PSP.
func hook(psp *fakePSP, id, ref, status, kind string) ([]byte, string) {
	body := []byte(fmt.Sprintf("%s|%s|%s|%s", id, ref, status, kind))
	return body, domain.SignWebhook(psp.secret, body)
}

// linkRef attaches a PSP ref to a stored payment, which the real flow does when
// the checkout is created.
func linkRef(st *fakeStore, paymentID, ref string) {
	p := st.payments[paymentID]
	p.PSPRef = ref
	st.payments[paymentID] = p
	st.byRef[ref] = paymentID
}

// ── purchase ────────────────────────────────────────────────────────────────

func TestStartPremiumRequiresConfiguredPSP(t *testing.T) {
	svc, _, _ := newSvc(false)
	if _, err := svc.StartPremium(context.Background(), who("alice"), 499, "USD"); codeOf(t, err) != "PAYMENTS_DISABLED" {
		t.Fatal("a deployment without a PSP must refuse rather than pretend")
	}
}

func TestStartPremiumCreatesPendingPayment(t *testing.T) {
	svc, st, _ := newSvc(true)
	co, err := svc.StartPremium(context.Background(), who("alice"), 499, "USD")
	if err != nil {
		t.Fatal(err)
	}
	if co.RedirectURL == "" || co.PaymentID == "" {
		t.Fatalf("checkout must carry a redirect and our payment id: %+v", co)
	}
	p := st.payments[co.PaymentID]
	if p.Status != domain.StatusPending {
		t.Fatalf("a new payment must start pending, got %s", p.Status)
	}
	// Nothing is granted before the webhook confirms.
	if ok, _ := svc.HasPremium(context.Background(), "alice"); ok {
		t.Fatal("premium must NOT be granted at checkout time — only on a confirmed payment")
	}
}

func TestPurchaseValidation(t *testing.T) {
	svc, _, _ := newSvc(true)
	ctx := context.Background()
	if _, err := svc.StartPremium(ctx, who("a"), 0, "USD"); codeOf(t, err) != "VALIDATION_AMOUNT" {
		t.Error("zero amount must be rejected")
	}
	if _, err := svc.StartPremium(ctx, who("a"), 100, "XYZ"); codeOf(t, err) != "VALIDATION_AMOUNT" {
		t.Error("unknown currency must be rejected")
	}
	if _, err := svc.StartChannelSubscription(ctx, who("a"), "", 100, "USD"); codeOf(t, err) != "VALIDATION_CHANNEL" {
		t.Error("channel subscription needs a channel")
	}
}

// The whole point of the context: a card number must never be accepted.
func TestCardDataIsRejectedAtTheBoundary(t *testing.T) {
	svc, _, _ := newSvc(true)
	if _, err := svc.StartChannelSubscription(context.Background(), who("a"), "4242424242424242", 100, "USD"); codeOf(t, err) != "VALIDATION_CARD_DATA" {
		t.Fatal("a PAN in any accepted field must be rejected")
	}
}

func TestPSPFailureLeavesPaymentPending(t *testing.T) {
	svc, st, psp := newSvc(true)
	psp.failNext = errors.New("processor down")
	if _, err := svc.StartPremium(context.Background(), who("alice"), 499, "USD"); codeOf(t, err) != "PAYMENTS_PSP_ERROR" {
		t.Fatal("a processor error should surface as a gateway error")
	}
	// It must NOT be marked failed: we do not know what the processor did, and
	// guessing risks losing a payment that actually went through.
	for _, p := range st.payments {
		if p.Status != domain.StatusPending {
			t.Fatalf("payment should stay pending for reconciliation, got %s", p.Status)
		}
	}
}

// ── webhooks ────────────────────────────────────────────────────────────────

func TestWebhookGrantsEntitlementOnSuccess(t *testing.T) {
	svc, st, psp := newSvc(true)
	ctx := context.Background()
	co, _ := svc.StartPremium(ctx, who("alice"), 499, "USD")
	linkRef(st, co.PaymentID, "ref1")

	body, sig := hook(psp, "evt1", "ref1", "succeeded", "payment.succeeded")
	if err := svc.HandleWebhook(ctx, body, sig); err != nil {
		t.Fatal(err)
	}
	if st.payments[co.PaymentID].Status != domain.StatusSucceeded {
		t.Fatal("payment should be succeeded")
	}
	ok, _ := svc.HasPremium(ctx, "alice")
	if !ok {
		t.Fatal("a confirmed payment must grant the entitlement")
	}
}

// A forged event must not grant anything — the single most important assertion
// in this package.
func TestWebhookRejectsForgedSignature(t *testing.T) {
	svc, st, psp := newSvc(true)
	ctx := context.Background()
	co, _ := svc.StartPremium(ctx, who("mallory"), 499, "USD")
	linkRef(st, co.PaymentID, "ref1")

	body, _ := hook(psp, "evt1", "ref1", "succeeded", "payment.succeeded")
	forged := domain.SignWebhook([]byte("attacker-secret"), body)
	if err := svc.HandleWebhook(ctx, body, forged); codeOf(t, err) != "WEBHOOK_SIGNATURE" {
		t.Fatal("a forged webhook must be rejected")
	}
	if ok, _ := svc.HasPremium(ctx, "mallory"); ok {
		t.Fatal("a forged webhook granted premium — critical failure")
	}
	if st.payments[co.PaymentID].Status != domain.StatusPending {
		t.Fatal("a forged webhook must not move the payment")
	}
}

func TestWebhookIsIdempotent(t *testing.T) {
	svc, st, psp := newSvc(true)
	ctx := context.Background()
	co, _ := svc.StartPremium(ctx, who("alice"), 499, "USD")
	linkRef(st, co.PaymentID, "ref1")

	body, sig := hook(psp, "evt1", "ref1", "succeeded", "payment.succeeded")
	for i := 0; i < 3; i++ {
		if err := svc.HandleWebhook(ctx, body, sig); err != nil {
			t.Fatalf("redelivery %d should ack, got %v", i, err)
		}
	}
	if st.updates != 1 {
		t.Fatalf("a redelivered event must run its side effects once, ran %d", st.updates)
	}
	if len(st.subs) != 1 {
		t.Fatalf("redelivery must not create duplicate subscriptions, got %d", len(st.subs))
	}
}

// PSPs deliver out of order; a late "pending" after a "succeeded" must not undo
// the settlement.
func TestWebhookIgnoresOutOfOrderTransition(t *testing.T) {
	svc, st, psp := newSvc(true)
	ctx := context.Background()
	co, _ := svc.StartPremium(ctx, who("alice"), 499, "USD")
	linkRef(st, co.PaymentID, "ref1")

	b1, s1 := hook(psp, "evt1", "ref1", "succeeded", "payment.succeeded")
	if err := svc.HandleWebhook(ctx, b1, s1); err != nil {
		t.Fatal(err)
	}
	b2, s2 := hook(psp, "evt2", "ref1", "failed", "payment.failed")
	if err := svc.HandleWebhook(ctx, b2, s2); err != nil {
		t.Fatalf("an illegal transition should be ignored, not error: %v", err)
	}
	if st.payments[co.PaymentID].Status != domain.StatusSucceeded {
		t.Fatal("a late failure event must not undo a settled payment")
	}
}

func TestWebhookForUnknownRefIsAcked(t *testing.T) {
	svc, _, psp := newSvc(true)
	body, sig := hook(psp, "evt9", "no-such-ref", "succeeded", "payment.succeeded")
	if err := svc.HandleWebhook(context.Background(), body, sig); err != nil {
		t.Fatalf("an unknown ref should ack (retrying will not help), got %v", err)
	}
}

// ── p2p ─────────────────────────────────────────────────────────────────────

func TestP2PDisabledByDefault(t *testing.T) {
	svc, _, _ := newSvc(true)
	_, err := svc.SendP2P(context.Background(), who("alice"), "bob", 500, "USD", "lunch")
	if codeOf(t, err) != "P2P_DISABLED" {
		t.Fatal("moving money between users must be off unless a licensed provider is wired")
	}
}

func TestP2PValidatesPayeeAndCardData(t *testing.T) {
	st, psp := newStore(), &fakePSP{secret: []byte("s"), enabled: true}
	svc := NewService(st, psp, fakeTransfers{enabled: true}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx := context.Background()
	if _, err := svc.SendP2P(ctx, who("alice"), "alice", 500, "USD", ""); codeOf(t, err) != "VALIDATION_PAYEE" {
		t.Error("sending to yourself must be rejected")
	}
	if _, err := svc.SendP2P(ctx, who("alice"), "bob", 500, "USD", "card 4242424242424242"); codeOf(t, err) != "VALIDATION_CARD_DATA" {
		t.Error("a PAN in the memo must be rejected")
	}
	if _, err := svc.SendP2P(ctx, who("alice"), "bob", 500, "USD", "lunch"); err != nil {
		t.Errorf("a valid transfer should go through: %v", err)
	}
}

// ── admin ───────────────────────────────────────────────────────────────────

func TestAdminRefundOnlyFromSucceeded(t *testing.T) {
	svc, st, psp := newSvc(true)
	ctx := context.Background()
	co, _ := svc.StartPremium(ctx, who("alice"), 499, "USD")
	linkRef(st, co.PaymentID, "ref1")

	// Pending is not refundable.
	if err := svc.AdminRefund(ctx, co.PaymentID); codeOf(t, err) != "STATE_NOT_REFUNDABLE" {
		t.Fatal("only a succeeded payment can be refunded")
	}
	body, sig := hook(psp, "evt1", "ref1", "succeeded", "payment.succeeded")
	_ = svc.HandleWebhook(ctx, body, sig)

	if err := svc.AdminRefund(ctx, co.PaymentID); err != nil {
		t.Fatalf("refunding a settled payment: %v", err)
	}
	if len(psp.refunds) != 1 || psp.refunds[0] != "ref1" {
		t.Fatalf("the processor must be told to refund the right ref: %+v", psp.refunds)
	}
	if st.payments[co.PaymentID].Status != domain.StatusRefunded {
		t.Fatal("payment should be refunded")
	}
	// And not twice.
	if err := svc.AdminRefund(ctx, co.PaymentID); codeOf(t, err) != "STATE_NOT_REFUNDABLE" {
		t.Fatal("a refunded payment must not be refundable again")
	}
}

func TestAdminRefundUnknownPayment(t *testing.T) {
	svc, _, _ := newSvc(true)
	if err := svc.AdminRefund(context.Background(), "nope"); codeOf(t, err) != "PAYMENT_NOT_FOUND" {
		t.Fatal("unknown payment should 404")
	}
}

func TestAdminListFiltersByStatus(t *testing.T) {
	svc, _, _ := newSvc(true)
	ctx := context.Background()
	if _, err := svc.AdminList(ctx, "nonsense", 10); codeOf(t, err) != "VALIDATION_STATUS" {
		t.Fatal("an unknown status filter must be rejected")
	}
	_, _ = svc.StartPremium(ctx, who("alice"), 499, "USD")
	got, err := svc.AdminList(ctx, "pending", 10)
	if err != nil || len(got) != 1 {
		t.Fatalf("expected the pending payment: %v %+v", err, got)
	}
}
