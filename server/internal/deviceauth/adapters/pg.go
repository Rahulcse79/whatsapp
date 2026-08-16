// Package adapters implements deviceauth.Store over PostgreSQL: passkey
// credentials, single-use WebAuthn challenges, and the login-event audit
// (passkey_credentials + webauthn_challenges + login_events; migration 000026).
package adapters

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/whatsapp-v2/server/internal/deviceauth"
)

type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func (s *Store) SaveChallenge(ctx context.Context, c deviceauth.Challenge) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO webauthn_challenges (value, user_id, purpose, expires_at) VALUES ($1, $2, $3, $4)`,
		c.Value, c.UserID, c.Purpose, c.ExpiresAt)
	return err
}

func (s *Store) TakeChallenge(ctx context.Context, value string) (deviceauth.Challenge, error) {
	var c deviceauth.Challenge
	err := s.pool.QueryRow(ctx,
		`DELETE FROM webauthn_challenges WHERE value = $1 RETURNING value, user_id, purpose, expires_at`, value).
		Scan(&c.Value, &c.UserID, &c.Purpose, &c.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return deviceauth.Challenge{}, deviceauth.ErrNotFound
	}
	return c, err
}

func (s *Store) CreateCredential(ctx context.Context, c deviceauth.Credential) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO passkey_credentials (id, user_id, alg, public_key, sign_count, name, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		c.ID, c.UserID, c.Alg, c.PublicKey, int64(c.SignCount), c.Name, c.CreatedAt)
	return err
}

func (s *Store) GetCredential(ctx context.Context, id string) (deviceauth.Credential, error) {
	var c deviceauth.Credential
	var signCount int64
	err := s.pool.QueryRow(ctx,
		`SELECT id, user_id, alg, public_key, sign_count, name, created_at, last_used_at
		 FROM passkey_credentials WHERE id = $1`, id).
		Scan(&c.ID, &c.UserID, &c.Alg, &c.PublicKey, &signCount, &c.Name, &c.CreatedAt, &c.LastUsedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return deviceauth.Credential{}, deviceauth.ErrNotFound
	}
	c.SignCount = uint32(signCount)
	return c, err
}

func (s *Store) ListCredentials(ctx context.Context, userID string) ([]deviceauth.Credential, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, user_id, alg, public_key, sign_count, name, created_at, last_used_at
		 FROM passkey_credentials WHERE user_id = $1 ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []deviceauth.Credential
	for rows.Next() {
		var c deviceauth.Credential
		var signCount int64
		if err := rows.Scan(&c.ID, &c.UserID, &c.Alg, &c.PublicKey, &signCount, &c.Name, &c.CreatedAt, &c.LastUsedAt); err != nil {
			return nil, err
		}
		c.SignCount = uint32(signCount)
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) UpdateSignCount(ctx context.Context, id string, count uint32, usedAt time.Time) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE passkey_credentials SET sign_count = $2, last_used_at = $3 WHERE id = $1`, id, int64(count), usedAt)
	return err
}

func (s *Store) DeleteCredential(ctx context.Context, userID, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM passkey_credentials WHERE id = $1 AND user_id = $2`, id, userID)
	return err
}

func (s *Store) RecordLogin(ctx context.Context, e deviceauth.LoginEvent) error {
	var deviceID *string
	if e.DeviceID != "" {
		deviceID = &e.DeviceID
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO login_events (id, user_id, device_id, ip, user_agent, at, suspicious)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		e.ID, e.UserID, deviceID, e.IP, e.UserAgent, e.At, e.Suspicious)
	return err
}

func (s *Store) KnownIPs(ctx context.Context, userID string) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT ip FROM login_events WHERE user_id = $1 AND ip <> ''`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var ip string
		if err := rows.Scan(&ip); err != nil {
			return nil, err
		}
		out = append(out, ip)
	}
	return out, rows.Err()
}

func (s *Store) RecentLogins(ctx context.Context, userID string, limit int) ([]deviceauth.LoginEvent, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, user_id, COALESCE(device_id::text, ''), ip, user_agent, at, suspicious
		 FROM login_events WHERE user_id = $1 ORDER BY at DESC LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []deviceauth.LoginEvent
	for rows.Next() {
		var e deviceauth.LoginEvent
		if err := rows.Scan(&e.ID, &e.UserID, &e.DeviceID, &e.IP, &e.UserAgent, &e.At, &e.Suspicious); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
