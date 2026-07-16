// Package adapters implements the keys context's persistence port. The
// one-time-prekey consume uses FOR UPDATE SKIP LOCKED so concurrent bundle
// fetches never hand out the same prekey twice — the correctness crux of
// this context (Docs/12-planning/task-breakdown.md T0.09 DONE).
package adapters

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/whatsapp-v2/server/internal/keys"
)

// PrekeyStore implements keys.Repo over PostgreSQL.
type PrekeyStore struct{ pool *pgxpool.Pool }

func NewPrekeyStore(pool *pgxpool.Pool) *PrekeyStore { return &PrekeyStore{pool: pool} }

func (s *PrekeyStore) ReplaceSignedPrekey(ctx context.Context, deviceID string, sp keys.SignedPrekey) error {
	// One current signed prekey per device: upsert on (device_id, key_id),
	// and the client rotates key_id forward. We keep history rows; the fetch
	// picks the highest key_id.
	_, err := s.pool.Exec(ctx, `
		INSERT INTO signed_prekeys (device_id, key_id, pubkey, signature)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (device_id, key_id) DO UPDATE
		  SET pubkey = EXCLUDED.pubkey, signature = EXCLUDED.signature`,
		deviceID, sp.KeyID, sp.Pubkey, sp.Signature)
	if err != nil {
		return fmt.Errorf("replacing signed prekey: %w", err)
	}
	return nil
}

func (s *PrekeyStore) AddOneTimePrekeys(ctx context.Context, deviceID string, otps []keys.OneTimePrekey) error {
	batch := &pgx.Batch{}
	for _, o := range otps {
		batch.Queue(`
			INSERT INTO prekeys (device_id, key_id, pubkey)
			VALUES ($1, $2, $3)
			ON CONFLICT (device_id, key_id) DO NOTHING`,
			deviceID, o.KeyID, o.Pubkey)
	}
	br := s.pool.SendBatch(ctx, batch)
	defer func() { _ = br.Close() }()
	for range otps {
		if _, err := br.Exec(); err != nil {
			return fmt.Errorf("adding one-time prekey: %w", err)
		}
	}
	return nil
}

func (s *PrekeyStore) CountAvailable(ctx context.Context, deviceID string) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM prekeys WHERE device_id = $1 AND consumed_at IS NULL`,
		deviceID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("counting available prekeys: %w", err)
	}
	return n, nil
}

func (s *PrekeyStore) ConsumeBundle(ctx context.Context, userID string) ([]keys.DeviceBundle, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin bundle tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, `
		SELECT id, identity_key FROM devices
		WHERE user_id = $1 AND revoked_at IS NULL`, userID)
	if err != nil {
		return nil, fmt.Errorf("listing devices: %w", err)
	}
	type dev struct {
		id string
		ik []byte
	}
	var devs []dev
	for rows.Next() {
		var d dev
		if err := rows.Scan(&d.id, &d.ik); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scanning device: %w", err)
		}
		devs = append(devs, d)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating devices: %w", err)
	}
	if len(devs) == 0 {
		return nil, keys.ErrNoDevices
	}

	bundles := make([]keys.DeviceBundle, 0, len(devs))
	for _, d := range devs {
		b := keys.DeviceBundle{DeviceID: d.id, IdentityKey: d.ik}

		// Highest key_id = the current signed prekey.
		err := tx.QueryRow(ctx, `
			SELECT key_id, pubkey, signature FROM signed_prekeys
			WHERE device_id = $1 ORDER BY key_id DESC LIMIT 1`, d.id).
			Scan(&b.SignedPrekey.KeyID, &b.SignedPrekey.Pubkey, &b.SignedPrekey.Signature)
		if errors.Is(err, pgx.ErrNoRows) {
			// A device that never published prekeys can't be a session target;
			// skip it rather than returning an unusable bundle.
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("loading signed prekey: %w", err)
		}

		// Atomically claim one one-time prekey. SKIP LOCKED is what makes
		// concurrent fetches take DISTINCT prekeys.
		var otp keys.OneTimePrekey
		err = tx.QueryRow(ctx, `
			WITH picked AS (
			  SELECT device_id, key_id FROM prekeys
			  WHERE device_id = $1 AND consumed_at IS NULL
			  ORDER BY key_id
			  FOR UPDATE SKIP LOCKED
			  LIMIT 1
			)
			UPDATE prekeys p SET consumed_at = now()
			FROM picked
			WHERE p.device_id = picked.device_id AND p.key_id = picked.key_id
			RETURNING p.key_id, p.pubkey`, d.id).
			Scan(&otp.KeyID, &otp.Pubkey)
		switch {
		case err == nil:
			b.OneTimePrekey = &otp
		case errors.Is(err, pgx.ErrNoRows):
			// Pool exhausted: bundle without a one-time prekey (allowed).
		default:
			return nil, fmt.Errorf("consuming one-time prekey: %w", err)
		}

		bundles = append(bundles, b)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit bundle tx: %w", err)
	}
	return bundles, nil
}
