package adapters

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/whatsapp-v2/server/internal/payments"
	"github.com/whatsapp-v2/server/internal/payments/domain"
	"github.com/whatsapp-v2/server/internal/platform/id"
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("WA_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("WA_TEST_PG_DSN not set — runs in the CI migrations job")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func seedUser(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	uid := id.New()
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO users (id, phone_hash) VALUES ($1, $2)`, uid, []byte("ph-"+uid)); err != nil {
		t.Fatal(err)
	}
	return uid
}

func newPayment(user string, purpose domain.Purpose, subject string) payments.Payment {
	now := time.Now()
	return payments.Payment{
		ID: id.New(), UserID: user, Purpose: purpose,
		Amount: domain.Money{Cents: 499, Currency: "USD"}, Status: domain.StatusPending,
		Subject: subject, CreatedAt: now, UpdatedAt: now,
	}
}

func TestIntegration_PaymentRoundTrip(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	s := NewStore(pool)
	u := seedUser(t, pool)

	p := newPayment(u, domain.PurposePremium, "")
	p.PSPRef = "ref-" + p.ID
	if err := s.CreatePayment(ctx, p); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetPayment(ctx, p.ID)
	if err != nil || got.Amount.Cents != 499 || got.Status != domain.StatusPending {
		t.Fatalf("round-trip: %v %+v", err, got)
	}
	byRef, err := s.GetPaymentByPSPRef(ctx, p.PSPRef)
	if err != nil || byRef.ID != p.ID {
		t.Fatalf("lookup by processor reference: %v %+v", err, byRef)
	}
	if _, err := s.GetPayment(ctx, id.New()); err != payments.ErrNotFound {
		t.Fatalf("unknown payment should be ErrNotFound, got %v", err)
	}
	list, err := s.ListPaymentsByUser(ctx, u, 10)
	if err != nil || len(list) != 1 {
		t.Fatalf("list by user: %v %d", err, len(list))
	}
}

// The entitlement and the payment that bought it are written together.
func TestIntegration_StatusAndSubscriptionAreAtomic(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	s := NewStore(pool)
	u := seedUser(t, pool)

	p := newPayment(u, domain.PurposePremium, "")
	p.PSPRef = "ref-" + p.ID
	if err := s.CreatePayment(ctx, p); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	sub := &payments.Subscription{
		ID: id.New(), UserID: u, Purpose: domain.PurposePremium, Subject: "",
		PSPRef: p.PSPRef, Active: true, StartedAt: now, ExpiresAt: now.Add(30 * 24 * time.Hour),
	}
	if err := s.UpdateStatus(ctx, p.ID, domain.StatusSucceeded, now, sub); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetPayment(ctx, p.ID)
	if got.Status != domain.StatusSucceeded {
		t.Fatalf("status not applied: %s", got.Status)
	}
	if _, err := s.ActiveSubscription(ctx, u, domain.PurposePremium, "", now); err != nil {
		t.Fatalf("the entitlement should exist alongside the payment: %v", err)
	}
}

// A second successful charge must extend the entitlement, not stack a second
// overlapping one (subscriptions_one_active).
func TestIntegration_RenewalExtendsRatherThanStacks(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	s := NewStore(pool)
	u := seedUser(t, pool)
	now := time.Now()

	for i := 0; i < 2; i++ {
		p := newPayment(u, domain.PurposeChannel, "chan-1")
		p.PSPRef = "ref-" + p.ID
		if err := s.CreatePayment(ctx, p); err != nil {
			t.Fatal(err)
		}
		sub := &payments.Subscription{
			ID: id.New(), UserID: u, Purpose: domain.PurposeChannel, Subject: "chan-1",
			PSPRef: p.PSPRef, Active: true, StartedAt: now,
			ExpiresAt: now.Add(time.Duration(30*(i+1)) * 24 * time.Hour),
		}
		if err := s.UpdateStatus(ctx, p.ID, domain.StatusSucceeded, now, sub); err != nil {
			t.Fatalf("charge %d: %v", i, err)
		}
	}
	subs, err := s.ListSubscriptionsByUser(ctx, u)
	if err != nil {
		t.Fatal(err)
	}
	active := 0
	for _, sb := range subs {
		if sb.Active && sb.CanceledAt == nil && sb.Subject == "chan-1" {
			active++
		}
	}
	if active != 1 {
		t.Fatalf("two charges must leave ONE active entitlement, got %d", active)
	}
	// And it must be the later expiry that survives.
	cur, err := s.ActiveSubscription(ctx, u, domain.PurposeChannel, "chan-1", now)
	if err != nil {
		t.Fatal(err)
	}
	if cur.ExpiresAt.Before(now.Add(59 * 24 * time.Hour)) {
		t.Fatalf("renewal should have extended the expiry, got %v", cur.ExpiresAt)
	}
}

// Cancelling stops renewal but does not revoke the paid period.
func TestIntegration_CancelKeepsPaidPeriod(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	s := NewStore(pool)
	u := seedUser(t, pool)
	now := time.Now()

	p := newPayment(u, domain.PurposePremium, "")
	p.PSPRef = "ref-" + p.ID
	_ = s.CreatePayment(ctx, p)
	sub := &payments.Subscription{
		ID: id.New(), UserID: u, Purpose: domain.PurposePremium,
		PSPRef: p.PSPRef, Active: true, StartedAt: now, ExpiresAt: now.Add(30 * 24 * time.Hour),
	}
	if err := s.UpdateStatus(ctx, p.ID, domain.StatusSucceeded, now, sub); err != nil {
		t.Fatal(err)
	}
	if err := s.CancelSubscription(ctx, u, sub.ID, now); err != nil {
		t.Fatal(err)
	}
	// ActiveSubscription excludes cancelled rows, which is the renewal signal;
	// the row itself is retained with its expiry for the record.
	if _, err := s.ActiveSubscription(ctx, u, domain.PurposePremium, "", now); err != payments.ErrNotFound {
		t.Fatalf("a cancelled subscription should not renew, got %v", err)
	}
	subs, _ := s.ListSubscriptionsByUser(ctx, u)
	if len(subs) != 1 || subs[0].CanceledAt == nil {
		t.Fatalf("the cancellation should be recorded, not the row deleted: %+v", subs)
	}
}

// The webhook idempotency gate: the same processor event id can only be
// recorded once, which is what stops a redelivery double-granting access.
func TestIntegration_EventIdempotency(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	s := NewStore(pool)
	evt := "evt-" + id.New()

	if err := s.MarkEventProcessed(ctx, evt, "ref1", "payment.succeeded", time.Now()); err != nil {
		t.Fatalf("first delivery should record: %v", err)
	}
	if err := s.MarkEventProcessed(ctx, evt, "ref1", "payment.succeeded", time.Now()); err != payments.ErrDuplicateEvt {
		t.Fatalf("a redelivered event must be reported as duplicate, got %v", err)
	}
}

func TestIntegration_AdminListFilters(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	s := NewStore(pool)
	u := seedUser(t, pool)

	p := newPayment(u, domain.PurposePremium, "")
	p.PSPRef = "ref-" + p.ID
	if err := s.CreatePayment(ctx, p); err != nil {
		t.Fatal(err)
	}
	pending, err := s.ListPayments(ctx, "pending", 100)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range pending {
		if row.ID == p.ID {
			found = true
		}
		if row.Status != domain.StatusPending {
			t.Fatalf("the status filter leaked a %s row", row.Status)
		}
	}
	if !found {
		t.Fatal("the new pending payment should appear in the filtered list")
	}
}
