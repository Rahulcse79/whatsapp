// Package adapters implements the payments ports: a PostgreSQL store
// (migration 000031) and the payment-provider adapters.
package adapters

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/whatsapp-v2/server/internal/payments"
	"github.com/whatsapp-v2/server/internal/payments/domain"
)

// uniqueViolation is Postgres' SQLSTATE for a duplicate key.
const uniqueViolation = "23505"

type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func (s *Store) CreatePayment(ctx context.Context, p payments.Payment) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO payments (id, user_id, purpose, amount_cents, currency, status, psp_ref, subject, memo, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		p.ID, p.UserID, string(p.Purpose), p.Amount.Cents, string(p.Amount.Currency),
		string(p.Status), nullStr(p.PSPRef), nullStr(p.Subject), nullStr(p.Memo), p.CreatedAt, p.UpdatedAt)
	return err
}

func (s *Store) GetPayment(ctx context.Context, id string) (payments.Payment, error) {
	return s.scanOne(ctx, `WHERE id = $1`, id)
}

func (s *Store) GetPaymentByPSPRef(ctx context.Context, ref string) (payments.Payment, error) {
	return s.scanOne(ctx, `WHERE psp_ref = $1`, ref)
}

const paymentCols = `id, user_id, purpose, amount_cents, currency, status,
                     COALESCE(psp_ref, ''), COALESCE(subject, ''), COALESCE(memo, ''), created_at, updated_at`

func (s *Store) scanOne(ctx context.Context, where string, arg any) (payments.Payment, error) {
	var (
		p        payments.Payment
		purpose  string
		currency string
		status   string
	)
	err := s.pool.QueryRow(ctx, `SELECT `+paymentCols+` FROM payments `+where, arg).
		Scan(&p.ID, &p.UserID, &purpose, &p.Amount.Cents, &currency, &status,
			&p.PSPRef, &p.Subject, &p.Memo, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return payments.Payment{}, payments.ErrNotFound
	}
	if err != nil {
		return payments.Payment{}, err
	}
	p.Purpose, p.Amount.Currency, p.Status = domain.Purpose(purpose), domain.Currency(currency), domain.Status(status)
	return p, nil
}

func (s *Store) ListPaymentsByUser(ctx context.Context, userID string, limit int) ([]payments.Payment, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+paymentCols+` FROM payments WHERE user_id = $1 ORDER BY created_at DESC LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPayments(rows)
}

func (s *Store) ListPayments(ctx context.Context, status string, limit int) ([]payments.Payment, error) {
	q := `SELECT ` + paymentCols + ` FROM payments`
	args := []any{limit}
	if status != "" {
		q += ` WHERE status = $2`
		args = append(args, status)
	}
	q += ` ORDER BY created_at DESC LIMIT $1`
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPayments(rows)
}

func scanPayments(rows pgx.Rows) ([]payments.Payment, error) {
	var out []payments.Payment
	for rows.Next() {
		var (
			p                         payments.Payment
			purpose, currency, status string
		)
		if err := rows.Scan(&p.ID, &p.UserID, &purpose, &p.Amount.Cents, &currency, &status,
			&p.PSPRef, &p.Subject, &p.Memo, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		p.Purpose, p.Amount.Currency, p.Status = domain.Purpose(purpose), domain.Currency(currency), domain.Status(status)
		out = append(out, p)
	}
	return out, rows.Err()
}

// UpdateStatus moves a payment and, when the payment grants access, writes the
// subscription IN THE SAME TRANSACTION. An entitlement that outlives its payment
// (or a payment with no entitlement) is the failure mode this prevents.
func (s *Store) UpdateStatus(ctx context.Context, paymentID string, to domain.Status, at time.Time, sub *payments.Subscription) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx,
		`UPDATE payments SET status = $2, updated_at = $3 WHERE id = $1`, paymentID, string(to), at); err != nil {
		return err
	}

	if sub != nil {
		// A renewal extends the existing row rather than stacking a second
		// active entitlement — subscriptions_one_active enforces that too.
		if _, err := tx.Exec(ctx,
			`INSERT INTO subscriptions (id, user_id, purpose, subject, psp_ref, active, started_at, expires_at)
			 VALUES ($1, $2, $3, $4, $5, true, $6, $7)
			 ON CONFLICT (user_id, purpose, subject) WHERE active AND canceled_at IS NULL
			 DO UPDATE SET expires_at = GREATEST(subscriptions.expires_at, EXCLUDED.expires_at),
			               psp_ref = EXCLUDED.psp_ref`,
			sub.ID, sub.UserID, string(sub.Purpose), sub.Subject, nullStr(sub.PSPRef),
			sub.StartedAt, sub.ExpiresAt); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) ListSubscriptionsByUser(ctx context.Context, userID string) ([]payments.Subscription, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, user_id, purpose, subject, COALESCE(psp_ref, ''), active, started_at, expires_at, canceled_at
		 FROM subscriptions WHERE user_id = $1 ORDER BY started_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []payments.Subscription
	for rows.Next() {
		var (
			sb      payments.Subscription
			purpose string
		)
		if err := rows.Scan(&sb.ID, &sb.UserID, &purpose, &sb.Subject, &sb.PSPRef,
			&sb.Active, &sb.StartedAt, &sb.ExpiresAt, &sb.CanceledAt); err != nil {
			return nil, err
		}
		sb.Purpose = domain.Purpose(purpose)
		out = append(out, sb)
	}
	return out, rows.Err()
}

func (s *Store) ActiveSubscription(ctx context.Context, userID string, purpose domain.Purpose, subject string, now time.Time) (payments.Subscription, error) {
	var (
		sb  payments.Subscription
		pur string
	)
	err := s.pool.QueryRow(ctx,
		`SELECT id, user_id, purpose, subject, COALESCE(psp_ref, ''), active, started_at, expires_at, canceled_at
		 FROM subscriptions
		 WHERE user_id = $1 AND purpose = $2 AND subject = $3
		   AND active AND canceled_at IS NULL AND expires_at > $4
		 ORDER BY expires_at DESC LIMIT 1`,
		userID, string(purpose), subject, now).
		Scan(&sb.ID, &sb.UserID, &pur, &sb.Subject, &sb.PSPRef, &sb.Active, &sb.StartedAt, &sb.ExpiresAt, &sb.CanceledAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return payments.Subscription{}, payments.ErrNotFound
	}
	if err != nil {
		return payments.Subscription{}, err
	}
	sb.Purpose = domain.Purpose(pur)
	return sb, nil
}

// CancelSubscription stops the renewal. `active` stays true and expires_at is
// untouched: the user paid for this period and keeps it.
func (s *Store) CancelSubscription(ctx context.Context, userID, subscriptionID string, at time.Time) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE subscriptions SET canceled_at = $3 WHERE id = $1 AND user_id = $2 AND canceled_at IS NULL`,
		subscriptionID, userID, at)
	return err
}

// MarkEventProcessed is the webhook idempotency gate: the PK does the work, so
// two concurrent deliveries of the same event cannot both proceed.
func (s *Store) MarkEventProcessed(ctx context.Context, eventID, pspRef, rawKind string, at time.Time) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO payment_events (event_id, psp_ref, raw_kind, processed_at) VALUES ($1, $2, $3, $4)`,
		eventID, nullStr(pspRef), rawKind, at)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return payments.ErrDuplicateEvt
	}
	return err
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

var _ payments.Store = (*Store)(nil)
