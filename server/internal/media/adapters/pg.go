// Package adapters implements media-svc's ports over PostgreSQL, MinIO, Valkey,
// NATS, and the core-api QuotaService gRPC.
package adapters

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/whatsapp-v2/server/internal/media"
	"github.com/whatsapp-v2/server/internal/media/domain"
)

// Store implements media.Store over media_objects.
type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func (s *Store) CreatePending(ctx context.Context, o media.Object) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO media_objects (id, object_key, size_bytes, content_hash, uploader_user_id, refcount, upload_state, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		o.ID, o.ObjectKey, o.SizeBytes, o.ContentHash, o.UploaderID, o.Refcount, int16(o.State), o.CreatedAt)
	return err
}

func scanObject(row pgx.Row) (media.Object, error) {
	var (
		o     media.Object
		state int16
	)
	err := row.Scan(&o.ID, &o.ObjectKey, &o.SizeBytes, &o.ContentHash, &o.UploaderID, &o.Refcount, &state, &o.CreatedAt, &o.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return media.Object{}, media.ErrNotFound
	}
	if err != nil {
		return media.Object{}, err
	}
	o.State = domain.UploadState(state)
	return o, nil
}

const selectObject = `SELECT id, object_key, size_bytes, content_hash, uploader_user_id, refcount, upload_state, created_at, expires_at FROM media_objects`

func (s *Store) Get(ctx context.Context, id string) (media.Object, error) {
	return scanObject(s.pool.QueryRow(ctx, selectObject+` WHERE id = $1`, id))
}

func (s *Store) GetByKey(ctx context.Context, key string) (media.Object, error) {
	return scanObject(s.pool.QueryRow(ctx, selectObject+` WHERE object_key = $1`, key))
}

func (s *Store) MarkComplete(ctx context.Context, id string, expiresAt time.Time) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE media_objects SET upload_state = 1, expires_at = $2 WHERE id = $1`, id, expiresAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return media.ErrNotFound
	}
	return nil
}

func (s *Store) AddRef(ctx context.Context, key string) (int32, error) {
	var rc int32
	err := s.pool.QueryRow(ctx,
		`UPDATE media_objects SET refcount = refcount + 1 WHERE object_key = $1 RETURNING refcount`, key).Scan(&rc)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, media.ErrNotFound
	}
	return rc, err
}

func (s *Store) DecRef(ctx context.Context, key string) (int32, error) {
	// At refcount 0 the object becomes GC-eligible 30 days later (last_ref + 30d).
	var rc int32
	err := s.pool.QueryRow(ctx,
		`UPDATE media_objects
		 SET refcount = GREATEST(refcount - 1, 0),
		     expires_at = CASE WHEN refcount - 1 <= 0 THEN now() + interval '30 days' ELSE expires_at END
		 WHERE object_key = $1 RETURNING refcount`, key).Scan(&rc)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, media.ErrNotFound
	}
	return rc, err
}

func (s *Store) SweepCandidates(ctx context.Context, now time.Time, limit int) ([]media.Object, error) {
	rows, err := s.pool.Query(ctx,
		selectObject+`
		 WHERE refcount = 0
		   AND ( (expires_at IS NOT NULL AND expires_at < $1)
		         OR (upload_state = 0 AND created_at < $1 - interval '24 hours') )
		 LIMIT $2`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []media.Object
	for rows.Next() {
		var (
			o     media.Object
			state int16
		)
		if err := rows.Scan(&o.ID, &o.ObjectKey, &o.SizeBytes, &o.ContentHash, &o.UploaderID, &o.Refcount, &state, &o.CreatedAt, &o.ExpiresAt); err != nil {
			return nil, err
		}
		o.State = domain.UploadState(state)
		out = append(out, o)
	}
	return out, rows.Err()
}

func (s *Store) Delete(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM media_objects WHERE id = $1`, id)
	return err
}
