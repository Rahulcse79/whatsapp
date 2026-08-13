// Package adapters implements the admin plane's stores — reports, the user
// metadata view, and the audit log — over PostgreSQL (migrations 000002,
// 000008, 000015). The invariant of security-architecture §4 is enforced here:
// every mutation writes its audit_log row in the SAME transaction, so an admin
// action can never land without its trace.
package adapters

import (
	"context"
	"encoding/hex"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/whatsapp-v2/server/internal/admin"
	"github.com/whatsapp-v2/server/internal/admin/domain"
)

// Store implements admin.Reports, admin.Users, and admin.Audit.
type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

var (
	_ admin.Reports   = (*Store)(nil)
	_ admin.Users     = (*Store)(nil)
	_ admin.Audit     = (*Store)(nil)
	_ admin.FlagStore = (*Store)(nil)
)

// ── reports ──────────────────────────────────────────────────────────────

const selectReport = `SELECT id, COALESCE(reporter_id::text, ''), COALESCE(target_user_id::text, ''),
	reason, COALESCE(note, ''), state, disclosed_ciphertext IS NOT NULL, created_at FROM reports`

func scanReport(row pgx.Row) (admin.Report, error) {
	var r admin.Report
	var state int16
	err := row.Scan(&r.ID, &r.ReporterID, &r.TargetUserID, &r.Reason, &r.Note, &state, &r.HasDisclosure, &r.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return admin.Report{}, admin.ErrNotFound
	}
	r.State = domain.ReportState(state)
	return r, err
}

// ListOpen returns the open queue oldest-first (FIFO fairness for T&S review).
func (s *Store) ListOpen(ctx context.Context, limit int) ([]admin.Report, error) {
	rows, err := s.pool.Query(ctx, selectReport+` WHERE state = 0 ORDER BY created_at ASC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []admin.Report
	for rows.Next() {
		r, err := scanReport(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) Get(ctx context.Context, id string) (admin.Report, error) {
	return scanReport(s.pool.QueryRow(ctx, selectReport+` WHERE id = $1`, id))
}

// Resolve moves an OPEN report to its final state, optionally suspends the
// target, and appends the audit row — one transaction. The state guard
// (state = 0) makes concurrent resolution of the same report a no-op for the
// loser, surfaced as not-found.
func (s *Store) Resolve(ctx context.Context, id string, state domain.ReportState, suspendUser string, audit admin.AuditEntry) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx, `UPDATE reports SET state = $2 WHERE id = $1 AND state = 0`, id, int16(state))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return admin.ErrNotFound
	}
	if suspendUser != "" {
		// Never resurrect a deleted tombstone (status 2); suspend only the live.
		if _, err := tx.Exec(ctx, `UPDATE users SET status = 1 WHERE id = $1 AND status <> 2`, suspendUser); err != nil {
			return err
		}
	}
	if err := appendAudit(ctx, tx, audit); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ── users (metadata view) ────────────────────────────────────────────────

// selectUser projects metadata only — never content — plus two aggregates:
// live device count and the number of reports filed against the user.
const selectUser = `SELECT u.id, COALESCE(u.username::text, ''), COALESCE(u.display_name, ''), u.status,
	(SELECT count(*) FROM devices d WHERE d.user_id = u.id AND d.revoked_at IS NULL),
	(SELECT count(*) FROM reports r WHERE r.target_user_id = u.id),
	u.created_at FROM users u`

func scanUser(row pgx.Row) (admin.UserSummary, error) {
	var u admin.UserSummary
	var dev, rep int64
	err := row.Scan(&u.ID, &u.Username, &u.DisplayName, &u.Status, &dev, &rep, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return admin.UserSummary{}, admin.ErrNotFound
	}
	u.DeviceCount, u.ReportCount = int(dev), int(rep)
	return u, err
}

// Search matches by username (trigram substring, users_username_trgm) or, when
// the query is the hex of a phone-hash, by exact phone_hash. Deleted tombstones
// (status 2) are excluded. No contact graph or content is ever exposed.
func (s *Store) Search(ctx context.Context, query string, limit int) ([]admin.UserSummary, error) {
	phoneHex := ""
	if _, err := hex.DecodeString(query); err == nil && len(query) >= 32 {
		phoneHex = query // valid hex of a plausible HMAC → try an exact phone_hash match
	}
	rows, err := s.pool.Query(ctx,
		selectUser+` WHERE u.status <> 2 AND (
			u.username ILIKE '%' || $1 || '%'
			OR ($2 <> '' AND u.phone_hash = decode($2, 'hex'))
		) ORDER BY u.created_at DESC LIMIT $3`,
		query, phoneHex, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []admin.UserSummary
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) Summary(ctx context.Context, userID string) (admin.UserSummary, error) {
	return scanUser(s.pool.QueryRow(ctx, selectUser+` WHERE u.id = $1`, userID))
}

// SetStatus updates a user's status and appends the audit row in one tx. A
// deleted tombstone (status 2) is immutable, so the update is a no-op there and
// surfaces as not-found rather than a silent success.
func (s *Store) SetStatus(ctx context.Context, userID string, status int16, audit admin.AuditEntry) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx, `UPDATE users SET status = $2 WHERE id = $1 AND status <> 2`, userID, status)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return admin.ErrNotFound
	}
	if err := appendAudit(ctx, tx, audit); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ── feature flags (management; core-api-lld §5) ──────────────────────────

// UpsertFlag writes a flag's rule and appends the audit row in one tx. updated_by
// is the admin's OIDC subject (text, migration 000016). The rule is bound as
// text and cast to jsonb.
func (s *Store) UpsertFlag(ctx context.Context, flag string, rules []byte, audit admin.AuditEntry) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx,
		`INSERT INTO feature_flags (flag, rules, updated_by, updated_at)
		 VALUES ($1, $2::jsonb, $3, now())
		 ON CONFLICT (flag) DO UPDATE SET rules = EXCLUDED.rules, updated_by = EXCLUDED.updated_by, updated_at = now()`,
		flag, string(rules), audit.Actor); err != nil {
		return err
	}
	if err := appendAudit(ctx, tx, audit); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// DeleteFlag removes a flag and appends the audit row in one tx. A missing flag
// surfaces as not-found rather than a silent success.
func (s *Store) DeleteFlag(ctx context.Context, flag string, audit admin.AuditEntry) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx, `DELETE FROM feature_flags WHERE flag = $1`, flag)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return admin.ErrNotFound
	}
	if err := appendAudit(ctx, tx, audit); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ── audit log ────────────────────────────────────────────────────────────

// appendAudit writes one append-only audit row. target "" becomes NULL. It runs
// inside the caller's transaction — that co-transactionality is the security
// guarantee, so there is no exported standalone Append.
func appendAudit(ctx context.Context, tx pgx.Tx, e admin.AuditEntry) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO audit_log (actor, action, target, reason) VALUES ($1, $2, $3, $4)`,
		e.Actor, e.Action, nullIfEmpty(e.Target), nullIfEmpty(e.Reason))
	return err
}

// List returns the newest audit rows for owner review.
func (s *Store) List(ctx context.Context, limit int) ([]admin.AuditRecord, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, actor, action, COALESCE(target::text, ''), COALESCE(reason, ''), at
		 FROM audit_log ORDER BY id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []admin.AuditRecord
	for rows.Next() {
		var a admin.AuditRecord
		if err := rows.Scan(&a.ID, &a.Actor, &a.Action, &a.Target, &a.Reason, &a.At); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
