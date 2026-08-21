// Package adapters implements whiteboard.Store over PostgreSQL (board_ops;
// migration 000028) with membership via conversation_members.
package adapters

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/whatsapp-v2/server/internal/whiteboard"
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

func (s *Store) AppendOp(ctx context.Context, o whiteboard.Op) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO board_ops (op_id, conversation_id, author, seq, kind, data)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (conversation_id, op_id) DO NOTHING`,
		o.ID, o.ConversationID, o.Author, o.Seq, o.Kind, []byte(o.Data))
	return err
}

func (s *Store) ListOps(ctx context.Context, conversationID string, since int64, limit int) ([]whiteboard.Op, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT op_id, conversation_id, author, seq, kind, data
		 FROM board_ops WHERE conversation_id = $1 AND seq > $2 ORDER BY seq, op_id LIMIT $3`,
		conversationID, since, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []whiteboard.Op
	for rows.Next() {
		var o whiteboard.Op
		var data []byte
		if err := rows.Scan(&o.ID, &o.ConversationID, &o.Author, &o.Seq, &o.Kind, &data); err != nil {
			return nil, err
		}
		o.Data = data
		out = append(out, o)
	}
	return out, rows.Err()
}

func (s *Store) MaxSeq(ctx context.Context, conversationID string) (int64, error) {
	var m int64
	err := s.pool.QueryRow(ctx, `SELECT COALESCE(max(seq), 0) FROM board_ops WHERE conversation_id = $1`, conversationID).Scan(&m)
	return m, err
}
