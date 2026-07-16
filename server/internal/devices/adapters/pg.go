// Package adapters implements the devices context's persistence and event
// ports over PostgreSQL and NATS.
package adapters

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/whatsapp-v2/server/internal/devices"
)

// Store implements devices.Repo and devices.LinkRepo over one pool.
type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func (s *Store) ListByUser(ctx context.Context, userID string) ([]devices.Device, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, user_id, is_primary, platform, COALESCE(device_name,''),
		       registered_at, last_active_at, revoked_at
		FROM devices WHERE user_id = $1 AND revoked_at IS NULL
		ORDER BY registered_at`, userID)
	if err != nil {
		return nil, fmt.Errorf("listing devices: %w", err)
	}
	defer rows.Close()

	var out []devices.Device
	for rows.Next() {
		var (
			d          devices.Device
			lastActive *time.Time
			revoked    *time.Time
		)
		if err := rows.Scan(&d.ID, &d.UserID, &d.IsPrimary, &d.Platform, &d.Name,
			&d.RegisteredAt, &lastActive, &revoked); err != nil {
			return nil, fmt.Errorf("scanning device: %w", err)
		}
		if lastActive != nil {
			d.LastActiveAt = *lastActive
		}
		d.Revoked = revoked != nil
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) Get(ctx context.Context, deviceID string) (devices.Device, error) {
	var (
		d          devices.Device
		lastActive *time.Time
		revoked    *time.Time
	)
	err := s.pool.QueryRow(ctx, `
		SELECT id, user_id, is_primary, platform, COALESCE(device_name,''),
		       registered_at, last_active_at, revoked_at
		FROM devices WHERE id = $1`, deviceID).
		Scan(&d.ID, &d.UserID, &d.IsPrimary, &d.Platform, &d.Name,
			&d.RegisteredAt, &lastActive, &revoked)
	if errors.Is(err, pgx.ErrNoRows) {
		return devices.Device{}, devices.ErrNotFound
	}
	if err != nil {
		return devices.Device{}, fmt.Errorf("loading device: %w", err)
	}
	if lastActive != nil {
		d.LastActiveAt = *lastActive
	}
	d.Revoked = revoked != nil
	return d, nil
}

func (s *Store) Rename(ctx context.Context, userID, deviceID, name string) (bool, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE devices SET device_name = $3
		WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL`, deviceID, userID, name)
	if err != nil {
		return false, fmt.Errorf("renaming device: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

func (s *Store) CountActive(ctx context.Context, userID string) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM devices WHERE user_id = $1 AND revoked_at IS NULL`,
		userID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("counting devices: %w", err)
	}
	return n, nil
}

// RevokeDevice tears down a device atomically. Revocation is inherently
// cross-context (it must invalidate auth sessions and keys prekeys too), so
// it runs as ONE transaction here — the only correct way to make it atomic.
func (s *Store) RevokeDevice(ctx context.Context, userID, deviceID string) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin revoke tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx, `
		UPDATE devices SET revoked_at = now()
		WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL`, deviceID, userID)
	if err != nil {
		return false, fmt.Errorf("revoking device: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return false, nil // not the user's device, or already revoked
	}

	// Sessions (auth), prekeys (keys), push token (notify) all die with the
	// device. FK ON DELETE CASCADE would fire on device delete, but we
	// tombstone (revoked_at) rather than delete, so we tear down explicitly.
	for _, stmt := range []string{
		`UPDATE sessions SET revoked_at = now() WHERE device_id = $1 AND revoked_at IS NULL`,
		`DELETE FROM prekeys WHERE device_id = $1`,
		`DELETE FROM signed_prekeys WHERE device_id = $1`,
		`DELETE FROM push_tokens WHERE device_id = $1`,
	} {
		if _, err := tx.Exec(ctx, stmt, deviceID); err != nil {
			return false, fmt.Errorf("tearing down device state: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit revoke tx: %w", err)
	}
	return true, nil
}

// ── LinkRepo ─────────────────────────────────────────────────────────────

func (s *Store) CreateLink(ctx context.Context, l devices.Link) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO device_links
		  (link_token, platform, device_name, identity_key, state, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		l.Token, l.Platform, l.Name, l.IdentityKey, int16(l.State), l.CreatedAt, l.ExpiresAt)
	if err != nil {
		return fmt.Errorf("creating device link: %w", err)
	}
	return nil
}

func (s *Store) GetLink(ctx context.Context, token string) (devices.Link, error) {
	var (
		l          devices.Link
		state      int16
		approvedBy *string
		userID     *string
		deviceID   *string
	)
	err := s.pool.QueryRow(ctx, `
		SELECT link_token, platform, COALESCE(device_name,''), identity_key, state,
		       approved_by, user_id, device_id, cert, created_at, expires_at
		FROM device_links WHERE link_token = $1`, token).
		Scan(&l.Token, &l.Platform, &l.Name, &l.IdentityKey, &state,
			&approvedBy, &userID, &deviceID, &l.Cert, &l.CreatedAt, &l.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return devices.Link{}, devices.ErrNotFound
	}
	if err != nil {
		return devices.Link{}, fmt.Errorf("loading device link: %w", err)
	}
	l.State = devices.LinkState(state)
	if approvedBy != nil {
		l.ApprovedBy = *approvedBy
	}
	if userID != nil {
		l.UserID = *userID
	}
	if deviceID != nil {
		l.DeviceID = *deviceID
	}
	return l, nil
}

// ApproveLink inserts the new linked device and flips the link to approved in
// one transaction. The state guard makes concurrent approvals safe: exactly
// one wins.
func (s *Store) ApproveLink(ctx context.Context, p devices.ApproveParams) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin approve tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx, `
		UPDATE device_links
		SET state = $2, approved_by = $3, user_id = $4, device_id = $5, cert = $6
		WHERE link_token = $1 AND state = $7`,
		p.Token, int16(devices.LinkApproved), p.ApprovedBy, p.UserID, p.NewDeviceID,
		p.Cert, int16(devices.LinkPending))
	if err != nil {
		return fmt.Errorf("approving link: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return devices.ErrNotFound // already handled or gone
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO devices
		  (id, user_id, is_primary, platform, device_name, identity_key, cert, registered_at)
		VALUES ($1, $2, false, $3, $4, $5, $6, $7)`,
		p.NewDeviceID, p.UserID, p.Platform, p.Name, p.IdentityKey, p.Cert, p.Now); err != nil {
		return fmt.Errorf("inserting linked device: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit approve tx: %w", err)
	}
	return nil
}

func (s *Store) ConsumeLink(ctx context.Context, token string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE device_links SET state = $2 WHERE link_token = $1`,
		token, int16(devices.LinkConsumed))
	if err != nil {
		return fmt.Errorf("consuming link: %w", err)
	}
	return nil
}
