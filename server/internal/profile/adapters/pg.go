// Package adapters implements the profile store over PostgreSQL (users + blocks,
// migrations 000002 / 000004). Identity metadata only — no message content.
package adapters

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/whatsapp-v2/server/internal/profile"
)

// Store implements profile.Store.
type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func (s *Store) Get(ctx context.Context, userID string) (profile.Profile, error) {
	var username, displayName, about, avatarRef *string
	var privacyJSON []byte
	err := s.pool.QueryRow(ctx,
		`SELECT username::text, display_name, about, avatar_ref, privacy FROM users WHERE id = $1`, userID).
		Scan(&username, &displayName, &about, &avatarRef, &privacyJSON)
	if errors.Is(err, pgx.ErrNoRows) {
		return profile.Profile{}, profile.ErrNotFound
	}
	if err != nil {
		return profile.Profile{}, err
	}
	p := profile.Profile{UserID: userID, Username: deref(username), DisplayName: deref(displayName), About: deref(about), AvatarRef: deref(avatarRef)}
	if len(privacyJSON) > 0 {
		_ = json.Unmarshal(privacyJSON, &p.Privacy)
	}
	return p, nil
}

func (s *Store) Public(ctx context.Context, userID string) (profile.Profile, error) {
	var username, displayName, about, avatarRef *string
	err := s.pool.QueryRow(ctx,
		`SELECT username::text, display_name, about, avatar_ref FROM users WHERE id = $1 AND status = 0`, userID).
		Scan(&username, &displayName, &about, &avatarRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return profile.Profile{}, profile.ErrNotFound
	}
	if err != nil {
		return profile.Profile{}, err
	}
	return profile.Profile{UserID: userID, Username: deref(username), DisplayName: deref(displayName), About: deref(about), AvatarRef: deref(avatarRef)}, nil
}

func (s *Store) Update(ctx context.Context, userID, displayName, username, about string) error {
	// A given username is written (case-folded via citext); empty keeps the
	// current one. display_name / about are set to the provided values.
	var un any
	if username != "" {
		un = strings.ToLower(username)
	}
	_, err := s.pool.Exec(ctx,
		`UPDATE users SET display_name = $2, about = $3, username = COALESCE($4, username) WHERE id = $1`,
		userID, displayName, about, un)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique_violation on username
		return profile.ErrUsernameTaken
	}
	return err
}

func (s *Store) SetPrivacy(ctx context.Context, userID string, privacy map[string]string) error {
	b, err := json.Marshal(privacy)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `UPDATE users SET privacy = $2 WHERE id = $1`, userID, b)
	return err
}

func (s *Store) Block(ctx context.Context, blocker, blocked string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO blocks (blocker_id, blocked_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, blocker, blocked)
	return err
}

func (s *Store) Unblock(ctx context.Context, blocker, blocked string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM blocks WHERE blocker_id = $1 AND blocked_id = $2`, blocker, blocked)
	return err
}

func (s *Store) Blocked(ctx context.Context, blocker string) ([]string, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT blocked_id::text FROM blocks WHERE blocker_id = $1 ORDER BY created_at DESC`, blocker)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// SetAvatar stores the profile picture's object key, or clears it when ref is
// empty. Replacing an avatar simply overwrites the key: the previous blob is
// left to MinIO lifecycle rather than deleted here, so a slow client still
// holding the old URL does not 404 mid-render.
func (s *Store) SetAvatar(ctx context.Context, userID, ref string) error {
	var v any
	if ref != "" {
		v = ref
	}
	_, err := s.pool.Exec(ctx, `UPDATE users SET avatar_ref = $2 WHERE id = $1`, userID, v)
	return err
}
