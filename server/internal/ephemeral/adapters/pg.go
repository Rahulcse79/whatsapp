// Package adapters implements ephemeral.Store over PostgreSQL: a per-conversation
// disappearing-timer registry (conversation_ephemeral; 000025), membership via
// conversation_members, and a backstop purge over the message_inbox relay buffer.
package adapters

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func (s *Store) IsMember(ctx context.Context, conversationID, userID string) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM conversation_members WHERE conversation_id = $1 AND user_id = $2)`,
		conversationID, userID).Scan(&ok)
	return ok, err
}

func (s *Store) GetTimer(ctx context.Context, conversationID string) (int, error) {
	var ttl int
	err := s.pool.QueryRow(ctx,
		`SELECT COALESCE((SELECT ttl_seconds FROM conversation_ephemeral WHERE conversation_id = $1), 0)`,
		conversationID).Scan(&ttl)
	return ttl, err
}

func (s *Store) SetTimer(ctx context.Context, conversationID string, ttlSeconds int, updatedBy string, at time.Time) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO conversation_ephemeral (conversation_id, ttl_seconds, updated_by, updated_at)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (conversation_id) DO UPDATE SET ttl_seconds = EXCLUDED.ttl_seconds, updated_by = EXCLUDED.updated_by, updated_at = EXCLUDED.updated_at`,
		conversationID, ttlSeconds, updatedBy, at)
	return err
}

// PurgeExpired removes relay ciphertext for disappearing conversations past their
// timer. A low-frequency backstop (the client enforces the UX); scoped to
// conversations that actually have a timer set, so undelivered messages in an
// ephemeral chat can't linger past the window.
func (s *Store) PurgeExpired(ctx context.Context, now time.Time) (int, error) {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM message_inbox mi
		 USING conversation_ephemeral ce
		 WHERE mi.conversation_id = ce.conversation_id
		   AND ce.ttl_seconds > 0
		   AND mi.accepted_at < $1::timestamptz - make_interval(secs => ce.ttl_seconds)`,
		now)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}
