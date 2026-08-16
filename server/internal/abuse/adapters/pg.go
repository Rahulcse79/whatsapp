// Package adapters implements abuse.Store over PostgreSQL: it files reports into
// the existing trust-and-safety queue (the reports table drained by the admin
// console, migration 000008) and checks a report target exists.
package adapters

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/whatsapp-v2/server/internal/abuse"
)

type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func (s *Store) FileReport(ctx context.Context, r abuse.Report) error {
	var note *string
	if r.Note != "" {
		note = &r.Note
	}
	var disclosed []byte
	if len(r.DisclosedCiphertext) > 0 {
		disclosed = r.DisclosedCiphertext
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO reports (id, reporter_id, target_user_id, reason, note, disclosed_ciphertext, state, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, 0, $7)`,
		r.ID, r.ReporterID, r.TargetUserID, int16(r.Reason), note, disclosed, r.CreatedAt)
	return err
}

func (s *Store) UserExists(ctx context.Context, userID string) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE id = $1)`, userID).Scan(&ok)
	return ok, err
}
