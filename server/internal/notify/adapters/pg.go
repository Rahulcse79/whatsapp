// Package adapters implements notify's ports: the token store over PostgreSQL,
// the push drivers, and the JetStream consumer + DLQ over NATS.
package adapters

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/whatsapp-v2/server/internal/notify"
)

// TokenStore reads and prunes device push tokens (push_tokens table).
type TokenStore struct{ pool *pgxpool.Pool }

func NewTokenStore(pool *pgxpool.Pool) *TokenStore { return &TokenStore{pool: pool} }

func (s *TokenStore) Resolve(ctx context.Context, deviceID string) (string, notify.Provider, error) {
	var (
		token    string
		provider int16
	)
	err := s.pool.QueryRow(ctx,
		`SELECT token, provider FROM push_tokens WHERE device_id = $1`, deviceID).
		Scan(&token, &provider)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", 0, notify.ErrNoToken
	}
	if err != nil {
		return "", 0, fmt.Errorf("resolving push token: %w", err)
	}
	return token, notify.Provider(provider), nil
}

func (s *TokenStore) Delete(ctx context.Context, deviceID string) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM push_tokens WHERE device_id = $1`, deviceID); err != nil {
		return fmt.Errorf("deleting push token: %w", err)
	}
	return nil
}

var _ notify.TokenStore = (*TokenStore)(nil)
