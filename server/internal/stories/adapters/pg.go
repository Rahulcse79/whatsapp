// Package adapters implements stories' Store (stories + story_views) and Audience
// (the author's contacts) over PostgreSQL.
package adapters

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/whatsapp-v2/server/internal/stories"
)

// Store implements stories.Store over the stories + story_views tables
// (migration 000006).
type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func (s *Store) Create(ctx context.Context, st stories.Story) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO stories (id, author_id, media_ref, audience_snapshot, expires_at, created_at)
		 VALUES ($1, $2, $3, $4::uuid[], $5, $6)`,
		st.ID, st.AuthorID, st.MediaRef, st.Audience, st.ExpiresAt, st.CreatedAt)
	return err
}

const selectStory = `SELECT id, author_id, media_ref, audience_snapshot, expires_at, created_at FROM stories`

func scanStory(row pgx.Row) (stories.Story, error) {
	var st stories.Story
	err := row.Scan(&st.ID, &st.AuthorID, &st.MediaRef, &st.Audience, &st.ExpiresAt, &st.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return stories.Story{}, stories.ErrNotFound
	}
	return st, err
}

func (s *Store) Get(ctx context.Context, id string) (stories.Story, error) {
	return scanStory(s.pool.QueryRow(ctx, selectStory+` WHERE id = $1`, id))
}

// Feed uses the GIN index on audience_snapshot for "stories that include me",
// excluding the expired.
func (s *Store) Feed(ctx context.Context, viewerID string, now time.Time) ([]stories.Story, error) {
	rows, err := s.pool.Query(ctx,
		selectStory+` WHERE audience_snapshot @> ARRAY[$1]::uuid[] AND expires_at > $2 ORDER BY created_at DESC`,
		viewerID, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []stories.Story
	for rows.Next() {
		st, err := scanStory(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

func (s *Store) RecordView(ctx context.Context, storyID, viewerID string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO story_views (story_id, viewer_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		storyID, viewerID)
	return err
}

func (s *Store) Viewers(ctx context.Context, storyID string) ([]stories.Viewer, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT viewer_id, viewed_at FROM story_views WHERE story_id = $1 ORDER BY viewed_at`, storyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []stories.Viewer
	for rows.Next() {
		var (
			v        stories.Viewer
			viewedAt time.Time
		)
		if err := rows.Scan(&v.UserID, &viewedAt); err != nil {
			return nil, err
		}
		v.ViewedAt = viewedAt.UnixMilli()
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) Delete(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM stories WHERE id = $1`, id) // story_views cascade
	return err
}

func (s *Store) PurgeExpired(ctx context.Context, now time.Time) (int, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM stories WHERE expires_at < $1`, now)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

var _ stories.Store = (*Store)(nil)

// Audience implements stories.Audience — the author's matched contacts (the
// default story recipients).
type Audience struct{ pool *pgxpool.Pool }

func NewAudience(pool *pgxpool.Pool) *Audience { return &Audience{pool: pool} }

func (a *Audience) ContactsOf(ctx context.Context, authorID string) ([]string, error) {
	rows, err := a.pool.Query(ctx,
		`SELECT DISTINCT matched_user_id FROM contacts WHERE owner_id = $1 AND matched_user_id IS NOT NULL`, authorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

var _ stories.Audience = (*Audience)(nil)
