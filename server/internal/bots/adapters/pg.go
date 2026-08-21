// Package adapters implements bots.Store over PostgreSQL (bots; migration 000029)
// and the HTTP webhook dispatcher.
package adapters

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/whatsapp-v2/server/internal/bots"
)

type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func (s *Store) Create(ctx context.Context, b bots.Bot) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO bots (id, owner_id, handle, name, webhook_url, secret, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		b.ID, b.OwnerID, b.Handle, b.Name, b.WebhookURL, b.Secret, b.CreatedAt)
	return err
}

func (s *Store) Get(ctx context.Context, id string) (bots.Bot, error) {
	return s.scanOne(ctx, `WHERE id = $1`, id)
}

func (s *Store) GetByHandle(ctx context.Context, handle string) (bots.Bot, error) {
	return s.scanOne(ctx, `WHERE handle = $1`, handle)
}

func (s *Store) scanOne(ctx context.Context, where string, arg any) (bots.Bot, error) {
	var b bots.Bot
	err := s.pool.QueryRow(ctx,
		`SELECT id, owner_id, handle, name, webhook_url, secret, created_at FROM bots `+where, arg).
		Scan(&b.ID, &b.OwnerID, &b.Handle, &b.Name, &b.WebhookURL, &b.Secret, &b.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return bots.Bot{}, bots.ErrNotFound
	}
	return b, err
}

func (s *Store) ListByOwner(ctx context.Context, ownerID string) ([]bots.Bot, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, owner_id, handle, name, webhook_url, secret, created_at FROM bots
		 WHERE owner_id = $1 ORDER BY created_at`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []bots.Bot
	for rows.Next() {
		var b bots.Bot
		if err := rows.Scan(&b.ID, &b.OwnerID, &b.Handle, &b.Name, &b.WebhookURL, &b.Secret, &b.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *Store) Delete(ctx context.Context, ownerID, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM bots WHERE id = $1 AND owner_id = $2`, id, ownerID)
	return err
}

func (s *Store) SetSecret(ctx context.Context, id, secret string) error {
	_, err := s.pool.Exec(ctx, `UPDATE bots SET secret = $2 WHERE id = $1`, id, secret)
	return err
}
