package payments

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/payments/domain"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
	"github.com/whatsapp-v2/server/internal/platform/id"
)

const (
	// SubscriptionPeriod is how long one successful charge grants access.
	SubscriptionPeriod = 30 * 24 * time.Hour
	maxMemoLen         = 200
	defaultListLimit   = 50
	maxListLimit       = 200
)

// Service is the payments control plane.
type Service struct {
	store     Store
	psp       PSP
	transfers Transfers
	log       *slog.Logger
	now       func() time.Time
	newID     func() string
}

func NewService(store Store, psp PSP, transfers Transfers, log *slog.Logger) *Service {
	return &Service{store: store, psp: psp, transfers: transfers, log: log, now: time.Now, newID: id.New}
}

// ── Buying things ───────────────────────────────────────────────────────────

// StartPremium begins an account-level premium subscription. It returns a
// checkout to redirect the payer to; the entitlement is granted only when the
// PSP's signed webhook confirms payment, never here.
func (s *Service) StartPremium(ctx context.Context, ident auth.Identity, cents int64, currency string) (Checkout, error) {
	return s.startPurchase(ctx, ident, domain.PurposePremium, "", cents, currency, "", true)
}

// StartChannelSubscription begins a paid-channel subscription.
func (s *Service) StartChannelSubscription(ctx context.Context, ident auth.Identity, channelID string, cents int64, currency string) (Checkout, error) {
	if strings.TrimSpace(channelID) == "" {
		return Checkout{}, httpx.Reject(http.StatusBadRequest, "VALIDATION_CHANNEL", "channel id is required")
	}
	return s.startPurchase(ctx, ident, domain.PurposeChannel, channelID, cents, currency, "", true)
}

func (s *Service) startPurchase(
	ctx context.Context, ident auth.Identity, purpose domain.Purpose,
	subject string, cents int64, currency, memo string, recurring bool,
) (Checkout, error) {
	if !s.psp.Enabled() {
		return Checkout{}, httpx.Reject(http.StatusServiceUnavailable, "PAYMENTS_DISABLED",
			"payments are not configured on this deployment")
	}
	amount, err := domain.NewMoney(cents, currency)
	if err != nil {
		return Checkout{}, httpx.Reject(http.StatusBadRequest, "VALIDATION_AMOUNT", err.Error())
	}
	// The card-data guard runs on every free-text field that could reach storage
	// or a log — this is what keeps the no-raw-card-data promise enforceable.
	if err := domain.RejectsCardDataIn(memo, subject); err != nil {
		return Checkout{}, httpx.Reject(http.StatusBadRequest, "VALIDATION_CARD_DATA", err.Error())
	}
	if len(memo) > maxMemoLen {
		return Checkout{}, httpx.Reject(http.StatusBadRequest, "VALIDATION_MEMO", "memo is too long")
	}

	now := s.now()
	p := Payment{
		ID: s.newID(), UserID: ident.UserID, Purpose: purpose, Amount: amount,
		Status: domain.StatusPending, Subject: subject, Memo: memo,
		CreatedAt: now, UpdatedAt: now,
	}
	// Record the intent BEFORE calling the PSP. If the PSP call succeeds but we
	// crash before writing, we would have taken money with no record of it —
	// the reverse (a pending row with no PSP intent) is harmless and expires.
	if err := s.store.CreatePayment(ctx, p); err != nil {
		return Checkout{}, httpx.Transient()
	}

	co, err := s.psp.CreateCheckout(ctx, Intent{
		PaymentID: p.ID, UserID: p.UserID, Purpose: purpose,
		Amount: amount, Subject: subject, Recurring: recurring,
	})
	if err != nil {
		s.log.Error("psp checkout failed", "payment_id", p.ID, "psp", s.psp.Name(), "err", err)
		// Leave the row pending: a reconciliation sweep or the PSP's own webhook
		// settles it. Never silently mark it failed on a local error — we do not
		// know what the processor did.
		return Checkout{}, httpx.Reject(http.StatusBadGateway, "PAYMENTS_PSP_ERROR", "the payment provider could not start a checkout")
	}
	co.PaymentID = p.ID
	return co, nil
}

// SendP2P is the person-to-person transfer seam. Moving money between users is
// a regulated activity, so the default build refuses: a deployment must inject
// a licensed provider.
func (s *Service) SendP2P(ctx context.Context, ident auth.Identity, toUserID string, cents int64, currency, memo string) (PaymentView, error) {
	if s.transfers == nil || !s.transfers.Enabled() {
		return PaymentView{}, httpx.Reject(http.StatusNotImplemented, "P2P_DISABLED", ErrP2PDisabled.Error())
	}
	if strings.TrimSpace(toUserID) == "" || toUserID == ident.UserID {
		return PaymentView{}, httpx.Reject(http.StatusBadRequest, "VALIDATION_PAYEE", "a different payee is required")
	}
	amount, err := domain.NewMoney(cents, currency)
	if err != nil {
		return PaymentView{}, httpx.Reject(http.StatusBadRequest, "VALIDATION_AMOUNT", err.Error())
	}
	if err := domain.RejectsCardData(memo); err != nil {
		return PaymentView{}, httpx.Reject(http.StatusBadRequest, "VALIDATION_CARD_DATA", err.Error())
	}
	if len(memo) > maxMemoLen {
		return PaymentView{}, httpx.Reject(http.StatusBadRequest, "VALIDATION_MEMO", "memo is too long")
	}

	now := s.now()
	p := Payment{
		ID: s.newID(), UserID: ident.UserID, Purpose: domain.PurposeP2P, Amount: amount,
		Status: domain.StatusPending, Subject: toUserID, Memo: memo, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.store.CreatePayment(ctx, p); err != nil {
		return PaymentView{}, httpx.Transient()
	}
	ref, err := s.transfers.Send(ctx, ident.UserID, toUserID, amount, memo)
	if err != nil {
		s.log.Error("p2p transfer failed", "payment_id", p.ID, "err", err)
		return PaymentView{}, httpx.Reject(http.StatusBadGateway, "P2P_PROVIDER_ERROR", "the transfer provider rejected the request")
	}
	p.PSPRef = ref
	return view(p), nil
}

// ── The webhook: where a payment actually becomes real ──────────────────────

// HandleWebhook authenticates and applies one PSP callback. `raw` must be the
// exact bytes received.
//
// Three properties this has to get right, because a PSP will exercise all of
// them: signature verification (forged events must not grant entitlements),
// idempotency (the same event arrives more than once), and ordering (events can
// arrive out of order, so an illegal transition is ignored rather than applied).
func (s *Service) HandleWebhook(ctx context.Context, raw []byte, signature string) error {
	ev, err := s.psp.VerifyWebhook(raw, signature)
	if err != nil {
		// 400, not 500: the sender is wrong, and a PSP should not retry this.
		return httpx.Reject(http.StatusBadRequest, "WEBHOOK_SIGNATURE", "signature verification failed")
	}

	now := s.now()
	// Idempotency gate first: a duplicate must not re-run any side effect.
	if err := s.store.MarkEventProcessed(ctx, ev.ID, ev.PSPRef, ev.RawKind, now); err != nil {
		if errors.Is(err, ErrDuplicateEvt) {
			return nil // already handled; ack so the PSP stops retrying
		}
		return httpx.Transient()
	}

	p, err := s.store.GetPaymentByPSPRef(ctx, ev.PSPRef)
	if errors.Is(err, ErrNotFound) {
		// An event for something we have no record of. Ack it — retrying will
		// not help — but log loudly: it means a checkout was created that we
		// failed to persist, which is a reconciliation problem.
		s.log.Error("webhook for unknown payment", "psp_ref", ev.PSPRef, "kind", ev.RawKind)
		return nil
	}
	if err != nil {
		return httpx.Transient()
	}

	changed, err := domain.Transition(p.Status, ev.Status)
	if err != nil {
		// Out-of-order or nonsensical: record and ignore rather than corrupt.
		s.log.Warn("ignoring illegal payment transition",
			"payment_id", p.ID, "from", p.Status, "to", ev.Status, "kind", ev.RawKind)
		return nil
	}
	if !changed {
		return nil
	}

	// A successful payment grants the entitlement, written in the same
	// transaction as the status change so the two can never disagree.
	var sub *Subscription
	if ev.Status == domain.StatusSucceeded && (p.Purpose == domain.PurposePremium || p.Purpose == domain.PurposeChannel) {
		sub = &Subscription{
			ID: s.newID(), UserID: p.UserID, Purpose: p.Purpose, Subject: p.Subject,
			PSPRef: p.PSPRef, Active: true, StartedAt: now, ExpiresAt: now.Add(SubscriptionPeriod),
		}
	}
	if err := s.store.UpdateStatus(ctx, p.ID, ev.Status, now, sub); err != nil {
		return httpx.Transient()
	}
	s.log.Info("payment settled", "payment_id", p.ID, "status", ev.Status, "purpose", p.Purpose)
	return nil
}

// ── Reads + entitlements ────────────────────────────────────────────────────

// MyPayments lists the caller's payment history.
func (s *Service) MyPayments(ctx context.Context, ident auth.Identity, limit int) ([]PaymentView, error) {
	ps, err := s.store.ListPaymentsByUser(ctx, ident.UserID, clamp(limit))
	if err != nil {
		return nil, httpx.Transient()
	}
	out := make([]PaymentView, len(ps))
	for i, p := range ps {
		out[i] = view(p)
	}
	return out, nil
}

// MySubscriptions lists the caller's subscriptions.
func (s *Service) MySubscriptions(ctx context.Context, ident auth.Identity) ([]SubscriptionView, error) {
	subs, err := s.store.ListSubscriptionsByUser(ctx, ident.UserID)
	if err != nil {
		return nil, httpx.Transient()
	}
	out := make([]SubscriptionView, len(subs))
	for i, sb := range subs {
		out[i] = subView(sb)
	}
	return out, nil
}

// HasPremium reports whether the user currently holds premium. This is the
// entitlement check other contexts call.
func (s *Service) HasPremium(ctx context.Context, userID string) (bool, error) {
	_, err := s.store.ActiveSubscription(ctx, userID, domain.PurposePremium, "", s.now())
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// HasChannelSubscription reports whether the user may read a paid channel.
func (s *Service) HasChannelSubscription(ctx context.Context, userID, channelID string) (bool, error) {
	_, err := s.store.ActiveSubscription(ctx, userID, domain.PurposeChannel, channelID, s.now())
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// CancelSubscription stops a subscription renewing. Access continues until the
// paid period ends — cancelling is not a refund.
func (s *Service) CancelSubscription(ctx context.Context, ident auth.Identity, subscriptionID string) error {
	if err := s.store.CancelSubscription(ctx, ident.UserID, subscriptionID, s.now()); err != nil {
		return httpx.Transient()
	}
	return nil
}

// ── Admin ───────────────────────────────────────────────────────────────────

// AdminList returns payments for the console, optionally filtered by status.
func (s *Service) AdminList(ctx context.Context, status string, limit int) ([]PaymentView, error) {
	if status != "" {
		if _, err := domain.ParseStatus(status); err != nil {
			return nil, httpx.Reject(http.StatusBadRequest, "VALIDATION_STATUS", err.Error())
		}
	}
	ps, err := s.store.ListPayments(ctx, status, clamp(limit))
	if err != nil {
		return nil, httpx.Transient()
	}
	out := make([]PaymentView, len(ps))
	for i, p := range ps {
		out[i] = view(p)
	}
	return out, nil
}

// AdminRefund reverses a settled payment. The caller is responsible for the
// authorisation check — this is invoked from the admin plane, which enforces
// its own RBAC and writes the audit entry.
func (s *Service) AdminRefund(ctx context.Context, paymentID string) error {
	p, err := s.store.GetPayment(ctx, paymentID)
	if errors.Is(err, ErrNotFound) {
		return httpx.Reject(http.StatusNotFound, "PAYMENT_NOT_FOUND", "payment not found")
	}
	if err != nil {
		return httpx.Transient()
	}
	// CanTransition, not Transition: Transition treats same→same as an
	// idempotent no-op (which is what a redelivered webhook needs), but an
	// operator refunding an already-refunded payment is a mistake to reject,
	// not a no-op to wave through.
	if !domain.CanTransition(p.Status, domain.StatusRefunded) {
		return httpx.Reject(http.StatusConflict, "STATE_NOT_REFUNDABLE",
			"only a succeeded payment can be refunded")
	}
	if err := s.psp.Refund(ctx, p.PSPRef); err != nil {
		return httpx.Reject(http.StatusBadGateway, "PAYMENTS_PSP_ERROR", "the payment provider rejected the refund")
	}
	// The refund webhook will also arrive; UpdateStatus is idempotent via the
	// state machine, so applying it here just makes the console feel immediate.
	if err := s.store.UpdateStatus(ctx, p.ID, domain.StatusRefunded, s.now(), nil); err != nil {
		return httpx.Transient()
	}
	return nil
}

func clamp(n int) int {
	if n <= 0 {
		return defaultListLimit
	}
	if n > maxListLimit {
		return maxListLimit
	}
	return n
}

func view(p Payment) PaymentView {
	return PaymentView{
		ID: p.ID, Purpose: string(p.Purpose), AmountCents: p.Amount.Cents,
		Currency: string(p.Amount.Currency), Status: string(p.Status),
		Subject: p.Subject, Memo: p.Memo,
		CreatedAtMS: p.CreatedAt.UnixMilli(), UpdatedAtMS: p.UpdatedAt.UnixMilli(),
	}
}

func subView(s Subscription) SubscriptionView {
	return SubscriptionView{
		ID: s.ID, Purpose: string(s.Purpose), Subject: s.Subject, Active: s.Active,
		StartedAtMS: s.StartedAt.UnixMilli(), ExpiresAtMS: s.ExpiresAt.UnixMilli(),
		Canceled: s.CanceledAt != nil,
	}
}
