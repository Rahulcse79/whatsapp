// Package adapters implements the backups registry over PostgreSQL.
package adapters

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/whatsapp-v2/server/internal/backups"
)

// Store implements backups.Store over the backups table (migration 000014).
type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func (s *Store) CreatePending(ctx context.Context, b backups.Backup) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO backups (id, user_id, object_key, size_bytes, upload_state, handle, created_at)
		 VALUES ($1, $2, $3, $4, 0, $5, $6)`,
		b.ID, b.UserID, b.ObjectKey, b.SizeBytes, b.Handle, b.CreatedAt)
	return err
}

const selectBackup = `SELECT id, user_id, object_key, size_bytes, COALESCE(handle, ''), created_at FROM backups`

func scanBackup(row pgx.Row) (backups.Backup, error) {
	var b backups.Backup
	err := row.Scan(&b.ID, &b.UserID, &b.ObjectKey, &b.SizeBytes, &b.Handle, &b.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return backups.Backup{}, backups.ErrNotFound
	}
	return b, err
}

func (s *Store) Get(ctx context.Context, id string) (backups.Backup, error) {
	return scanBackup(s.pool.QueryRow(ctx, selectBackup+` WHERE id = $1`, id))
}

func (s *Store) MarkComplete(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE backups SET upload_state = 1, handle = NULL WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return backups.ErrNotFound
	}
	return nil
}

func (s *Store) Latest(ctx context.Context, userID string) (backups.Backup, error) {
	return scanBackup(s.pool.QueryRow(ctx,
		selectBackup+` WHERE user_id = $1 AND upload_state = 1 ORDER BY created_at DESC LIMIT 1`, userID))
}

func (s *Store) OldComplete(ctx context.Context, userID, exceptID string) ([]backups.Backup, error) {
	rows, err := s.pool.Query(ctx,
		selectBackup+` WHERE user_id = $1 AND upload_state = 1 AND id <> $2`, userID, exceptID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []backups.Backup
	for rows.Next() {
		b, err := scanBackup(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *Store) Delete(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM backups WHERE id = $1`, id)
	return err
}

var _ backups.Store = (*Store)(nil)
