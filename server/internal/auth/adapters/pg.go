// Package adapters implements auth's ports over real infrastructure:
// PostgreSQL repositories and OTP delivery drivers. No business decisions
// live here — those are in domain/ and service.go.
package adapters

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/auth/domain"
)

// Store bundles all PG-backed auth repositories over one pool.
type Store struct {
	Challenges *Challenges
	Users      *Users
	Sessions   *Sessions
	Registrar  *Registrar
	Attempts   *Attempts
}

// NewStore wires every repository to pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{
		Challenges: &Challenges{pool: pool},
		Users:      &Users{pool: pool},
		Sessions:   &Sessions{pool: pool},
		Registrar:  &Registrar{pool: pool},
		Attempts:   &Attempts{pool: pool},
	}
}

// ── Challenges ───────────────────────────────────────────────────────────

type Challenges struct{ pool *pgxpool.Pool }

func (c *Challenges) Create(ctx context.Context, ch domain.Challenge) error {
	_, err := c.pool.Exec(ctx, `
		INSERT INTO otp_challenges
		  (id, phone_hash, code_hash, salt, channel, attempts, pin_pending, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		ch.ID, ch.PhoneHash, ch.CodeHash, ch.Salt, int16(ch.Channel),
		int16(ch.Attempts), ch.PinPending, ch.CreatedAt, ch.ExpiresAt)
	if err != nil {
		return fmt.Errorf("creating otp challenge: %w", err)
	}
	return nil
}

func (c *Challenges) Get(ctx context.Context, id string) (domain.Challenge, error) {
	var (
		ch         domain.Challenge
		channel    int16
		attempts   int16
		verifiedAt *time.Time
	)
	err := c.pool.QueryRow(ctx, `
		SELECT id, phone_hash, code_hash, salt, channel, attempts, verified_at,
		       pin_pending, created_at, expires_at
		FROM otp_challenges WHERE id = $1`, id).
		Scan(&ch.ID, &ch.PhoneHash, &ch.CodeHash, &ch.Salt, &channel, &attempts,
			&verifiedAt, &ch.PinPending, &ch.CreatedAt, &ch.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Challenge{}, auth.ErrNotFound
	}
	if err != nil {
		return domain.Challenge{}, fmt.Errorf("loading otp challenge: %w", err)
	}
	ch.Channel = domain.Channel(channel)
	ch.Attempts = int(attempts)
	if verifiedAt != nil {
		ch.VerifiedAt = *verifiedAt
	}
	return ch, nil
}

func (c *Challenges) IncrementAttempts(ctx context.Context, id string) error {
	_, err := c.pool.Exec(ctx,
		`UPDATE otp_challenges SET attempts = attempts + 1 WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("incrementing otp attempts: %w", err)
	}
	return nil
}

func (c *Challenges) MarkVerified(ctx context.Context, id string, pinPending bool, at time.Time) error {
	_, err := c.pool.Exec(ctx,
		`UPDATE otp_challenges SET verified_at = $2, pin_pending = $3 WHERE id = $1`,
		id, at, pinPending)
	if err != nil {
		return fmt.Errorf("marking otp challenge verified: %w", err)
	}
	return nil
}

// ── Users ────────────────────────────────────────────────────────────────

type Users struct{ pool *pgxpool.Pool }

func (u *Users) ByPhoneHash(ctx context.Context, phoneHash []byte) (auth.User, error) {
	return u.one(ctx,
		`SELECT id, COALESCE(pin_hash, ''), status FROM users
		 WHERE phone_hash = $1 AND deleted_at IS NULL`, phoneHash)
}

func (u *Users) ByID(ctx context.Context, id string) (auth.User, error) {
	return u.one(ctx,
		`SELECT id, COALESCE(pin_hash, ''), status FROM users
		 WHERE id = $1 AND deleted_at IS NULL`, id)
}

func (u *Users) one(ctx context.Context, sql string, arg any) (auth.User, error) {
	var usr auth.User
	err := u.pool.QueryRow(ctx, sql, arg).Scan(&usr.ID, &usr.PINHash, &usr.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.User{}, auth.ErrNotFound
	}
	if err != nil {
		return auth.User{}, fmt.Errorf("loading user: %w", err)
	}
	return usr, nil
}

func (u *Users) SetPINHash(ctx context.Context, userID, phc string) error {
	tag, err := u.pool.Exec(ctx,
		`UPDATE users SET pin_hash = $2 WHERE id = $1 AND deleted_at IS NULL`, userID, phc)
	if err != nil {
		return fmt.Errorf("setting pin hash: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return auth.ErrNotFound
	}
	return nil
}

// ── Sessions ─────────────────────────────────────────────────────────────

type Sessions struct{ pool *pgxpool.Pool }

const sessionSelect = `
	SELECT s.id, s.device_id, d.user_id, s.refresh_hash, s.rotated_from,
	       s.expires_at, s.revoked_at
	FROM sessions s JOIN devices d ON d.id = s.device_id `

func (s *Sessions) ByRefreshHash(ctx context.Context, hash []byte) (domain.Session, error) {
	return s.one(ctx, sessionSelect+`WHERE s.refresh_hash = $1`, hash)
}

func (s *Sessions) ByRotatedFrom(ctx context.Context, hash []byte) (domain.Session, error) {
	return s.one(ctx, sessionSelect+`WHERE s.rotated_from = $1 AND s.revoked_at IS NULL`, hash)
}

func (s *Sessions) one(ctx context.Context, sql string, arg any) (domain.Session, error) {
	var (
		sess      domain.Session
		revokedAt *time.Time
	)
	err := s.pool.QueryRow(ctx, sql, arg).Scan(
		&sess.ID, &sess.DeviceID, &sess.UserID, &sess.RefreshHash,
		&sess.RotatedFrom, &sess.ExpiresAt, &revokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Session{}, auth.ErrNotFound
	}
	if err != nil {
		return domain.Session{}, fmt.Errorf("loading session: %w", err)
	}
	if revokedAt != nil {
		sess.RevokedAt = *revokedAt
	}
	return sess, nil
}

func (s *Sessions) Rotate(ctx context.Context, sessionID string, oldHash, newHash []byte, at time.Time) (bool, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE sessions SET refresh_hash = $3, rotated_from = $2, last_used_at = $4
		WHERE id = $1 AND refresh_hash = $2 AND revoked_at IS NULL`,
		sessionID, oldHash, newHash, at)
	if err != nil {
		return false, fmt.Errorf("rotating session: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

func (s *Sessions) Revoke(ctx context.Context, sessionID string, at time.Time) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE sessions SET revoked_at = $2 WHERE id = $1 AND revoked_at IS NULL`,
		sessionID, at)
	if err != nil {
		return fmt.Errorf("revoking session: %w", err)
	}
	return nil
}

// ── Registrar (the registration transaction) ─────────────────────────────

type Registrar struct{ pool *pgxpool.Pool }

const maxDevicesPerUser = 5 // 1 primary + 4 linked (FR-AUTH-05)

func (r *Registrar) RegisterDevice(ctx context.Context, p auth.RegisterDeviceParams) (auth.RegisterDeviceResult, error) {
	res, err := r.try(ctx, p)
	if err != nil && isUniqueViolation(err) {
		// Two first-registrations raced on phone_hash; the user row exists
		// now — retry once and join it.
		return r.try(ctx, p)
	}
	return res, err
}

func (r *Registrar) try(ctx context.Context, p auth.RegisterDeviceParams) (auth.RegisterDeviceResult, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return auth.RegisterDeviceResult{}, fmt.Errorf("begin register tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var (
		userID  string
		newUser bool
	)
	err = tx.QueryRow(ctx,
		`SELECT id FROM users WHERE phone_hash = $1 AND deleted_at IS NULL FOR UPDATE`,
		p.PhoneHash).Scan(&userID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		newUser = true
		userID = p.UserID
		if _, err := tx.Exec(ctx,
			`INSERT INTO users (id, phone_hash) VALUES ($1, $2)`, userID, p.PhoneHash); err != nil {
			return auth.RegisterDeviceResult{}, err // 23505 bubbles up for the retry
		}
	case err != nil:
		return auth.RegisterDeviceResult{}, fmt.Errorf("locking user: %w", err)
	}

	var active int
	if err := tx.QueryRow(ctx,
		`SELECT count(*) FROM devices WHERE user_id = $1 AND revoked_at IS NULL`,
		userID).Scan(&active); err != nil {
		return auth.RegisterDeviceResult{}, fmt.Errorf("counting devices: %w", err)
	}
	if active >= maxDevicesPerUser {
		return auth.RegisterDeviceResult{}, auth.ErrDeviceLimit
	}

	// cert: the primary-signed device certificate flow lands with T0.09;
	// until then the identity key stands in so the NOT NULL contract holds.
	if _, err := tx.Exec(ctx, `
		INSERT INTO devices (id, user_id, is_primary, platform, device_name, identity_key, cert, registered_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		p.DeviceID, userID, active == 0, p.Platform, p.DeviceName,
		p.IdentityKey, p.IdentityKey, p.Now); err != nil {
		return auth.RegisterDeviceResult{}, fmt.Errorf("inserting device: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO sessions (id, device_id, refresh_hash, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5)`,
		p.SessionID, p.DeviceID, p.RefreshHash, p.Now, p.SessionExpiresAt); err != nil {
		return auth.RegisterDeviceResult{}, fmt.Errorf("inserting session: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return auth.RegisterDeviceResult{}, fmt.Errorf("commit register tx: %w", err)
	}
	return auth.RegisterDeviceResult{UserID: userID, NewUser: newUser}, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// ── Attempts (abuse audit) ───────────────────────────────────────────────

type Attempts struct{ pool *pgxpool.Pool }

func (a *Attempts) Record(ctx context.Context, phoneHash []byte, success bool, at time.Time) error {
	_, err := a.pool.Exec(ctx, `
		INSERT INTO otp_attempts (phone_hash, at, success)
		VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`, phoneHash, at, success)
	if err != nil {
		return fmt.Errorf("recording otp attempt: %w", err)
	}
	return nil
}
