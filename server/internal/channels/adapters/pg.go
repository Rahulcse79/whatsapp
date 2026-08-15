// Package adapters implements channels.Store over PostgreSQL (migration 000019)
// and channels.Broadcaster over NATS.
package adapters

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/whatsapp-v2/server/internal/channels"
	"github.com/whatsapp-v2/server/internal/channels/domain"
)

// Store implements channels.Store.
type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

const channelCols = `id, owner_id::text, handle, name, COALESCE(description,''), kind, verified, created_at`

func scanChannel(row pgx.Row) (channels.Channel, error) {
	var (
		c    channels.Channel
		kind int16
	)
	err := row.Scan(&c.ID, &c.OwnerID, &c.Handle, &c.Name, &c.Description, &kind, &c.Verified, &c.CreatedAt)
	c.Kind = domain.Kind(kind)
	return c, err
}

func (s *Store) CreateChannel(ctx context.Context, c channels.Channel) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO channels (id, owner_id, handle, name, description, kind, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		c.ID, c.OwnerID, c.Handle, c.Name, c.Description, int16(c.Kind), c.CreatedAt)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return channels.ErrHandleTaken
	}
	return err
}

func (s *Store) GetChannel(ctx context.Context, id string) (channels.Channel, error) {
	c, err := scanChannel(s.pool.QueryRow(ctx, `SELECT `+channelCols+` FROM channels WHERE id = $1 AND deleted_at IS NULL`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return channels.Channel{}, channels.ErrNotFound
	}
	return c, err
}

func (s *Store) UpdateChannel(ctx context.Context, id string, name, description *string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE channels SET name = COALESCE($2, name), description = COALESCE($3, description)
		 WHERE id = $1 AND deleted_at IS NULL`, id, name, description)
	return err
}

func (s *Store) DeleteChannel(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `UPDATE channels SET deleted_at = now() WHERE id = $1 AND deleted_at IS NULL`, id)
	return err
}

func (s *Store) SearchChannels(ctx context.Context, query string, limit int) ([]channels.Channel, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+channelCols+` FROM channels
		 WHERE kind = 0 AND deleted_at IS NULL AND (name ILIKE '%' || $1 || '%' OR handle ILIKE '%' || $1 || '%')
		 ORDER BY name LIMIT $2`, query, limit)
	if err != nil {
		return nil, err
	}
	return collectChannels(rows)
}

func (s *Store) Discover(ctx context.Context, limit int) ([]channels.Channel, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+channelCols+`,
		        (SELECT count(*) FROM channel_members m WHERE m.channel_id = channels.id) AS followers
		 FROM channels WHERE kind = 0 AND deleted_at IS NULL
		 ORDER BY followers DESC, created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []channels.Channel
	for rows.Next() {
		var (
			c         channels.Channel
			kind      int16
			followers int
		)
		if err := rows.Scan(&c.ID, &c.OwnerID, &c.Handle, &c.Name, &c.Description, &kind, &c.Verified, &c.CreatedAt, &followers); err != nil {
			return nil, err
		}
		c.Kind = domain.Kind(kind)
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) FollowerCount(ctx context.Context, channelID string) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM channel_members WHERE channel_id = $1`, channelID).Scan(&n)
	return n, err
}

func (s *Store) GetMember(ctx context.Context, channelID, userID string) (channels.Member, error) {
	var (
		m    = channels.Member{ChannelID: channelID, UserID: userID}
		role int16
	)
	err := s.pool.QueryRow(ctx,
		`SELECT role, joined_at FROM channel_members WHERE channel_id = $1 AND user_id = $2`, channelID, userID).
		Scan(&role, &m.JoinedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return channels.Member{}, channels.ErrNotFound
	}
	m.Role = domain.Role(role)
	return m, err
}

func (s *Store) AddMember(ctx context.Context, m channels.Member) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO channel_members (channel_id, user_id, role, joined_at) VALUES ($1, $2, $3, $4)
		 ON CONFLICT (channel_id, user_id) DO NOTHING`,
		m.ChannelID, m.UserID, int16(m.Role), m.JoinedAt)
	return err
}

func (s *Store) RemoveMember(ctx context.Context, channelID, userID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM channel_members WHERE channel_id = $1 AND user_id = $2`, channelID, userID)
	return err
}

func (s *Store) SetRole(ctx context.Context, channelID, userID string, role domain.Role) error {
	tag, err := s.pool.Exec(ctx, `UPDATE channel_members SET role = $3 WHERE channel_id = $1 AND user_id = $2`, channelID, userID, int16(role))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return channels.ErrNotFound
	}
	return nil
}

func (s *Store) ListMembers(ctx context.Context, channelID string, limit int) ([]channels.Member, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT user_id::text, role, joined_at FROM channel_members WHERE channel_id = $1
		 ORDER BY role DESC, joined_at LIMIT $2`, channelID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []channels.Member
	for rows.Next() {
		var (
			m    = channels.Member{ChannelID: channelID}
			role int16
		)
		if err := rows.Scan(&m.UserID, &role, &m.JoinedAt); err != nil {
			return nil, err
		}
		m.Role = domain.Role(role)
		out = append(out, m)
	}
	return out, rows.Err()
}

const postCols = `id, channel_id::text, author_id::text, body, media_ref::text, publish_at, published, created_at`

func scanPost(row pgx.Row) (channels.Post, error) {
	var p channels.Post
	err := row.Scan(&p.ID, &p.ChannelID, &p.AuthorID, &p.Body, &p.MediaRef, &p.PublishAt, &p.Published, &p.CreatedAt)
	return p, err
}

func (s *Store) CreatePost(ctx context.Context, p channels.Post) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO channel_posts (id, channel_id, author_id, body, media_ref, publish_at, published, created_at)
		 VALUES ($1, $2, $3, $4, $5::uuid, $6, $7, $8)`,
		p.ID, p.ChannelID, p.AuthorID, p.Body, p.MediaRef, p.PublishAt, p.Published, p.CreatedAt)
	return err
}

func (s *Store) GetPost(ctx context.Context, postID string) (channels.Post, error) {
	p, err := scanPost(s.pool.QueryRow(ctx, `SELECT `+postCols+` FROM channel_posts WHERE id = $1 AND deleted_at IS NULL`, postID))
	if errors.Is(err, pgx.ErrNoRows) {
		return channels.Post{}, channels.ErrNotFound
	}
	return p, err
}

func (s *Store) ListPosts(ctx context.Context, channelID string, limit int) ([]channels.Post, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+postCols+` FROM channel_posts
		 WHERE channel_id = $1 AND deleted_at IS NULL AND published
		 ORDER BY publish_at DESC LIMIT $2`, channelID, limit)
	if err != nil {
		return nil, err
	}
	return collectPosts(rows)
}

func (s *Store) DeletePost(ctx context.Context, postID string) error {
	_, err := s.pool.Exec(ctx, `UPDATE channel_posts SET deleted_at = now() WHERE id = $1 AND deleted_at IS NULL`, postID)
	return err
}

func (s *Store) PublishDue(ctx context.Context, now time.Time, limit int) ([]channels.Post, error) {
	rows, err := s.pool.Query(ctx,
		`UPDATE channel_posts SET published = true
		 WHERE id IN (
		   SELECT id FROM channel_posts WHERE NOT published AND deleted_at IS NULL AND publish_at <= $1
		   ORDER BY publish_at LIMIT $2
		 )
		 RETURNING `+postCols, now, limit)
	if err != nil {
		return nil, err
	}
	return collectPosts(rows)
}

func (s *Store) React(ctx context.Context, postID, userID, emoji string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO channel_post_reactions (post_id, user_id, emoji) VALUES ($1, $2, $3)
		 ON CONFLICT DO NOTHING`, postID, userID, emoji)
	return err
}

func (s *Store) Unreact(ctx context.Context, postID, userID, emoji string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM channel_post_reactions WHERE post_id = $1 AND user_id = $2 AND emoji = $3`, postID, userID, emoji)
	return err
}

func (s *Store) Reactions(ctx context.Context, postID string) (map[string]int, error) {
	rows, err := s.pool.Query(ctx, `SELECT emoji, count(*) FROM channel_post_reactions WHERE post_id = $1 GROUP BY emoji`, postID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var (
			emoji string
			n     int
		)
		if err := rows.Scan(&emoji, &n); err != nil {
			return nil, err
		}
		out[emoji] = n
	}
	return out, rows.Err()
}

func (s *Store) CreateComment(ctx context.Context, c channels.Comment) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO channel_comments (id, post_id, author_id, body, created_at) VALUES ($1, $2, $3, $4, $5)`,
		c.ID, c.PostID, c.AuthorID, c.Body, c.CreatedAt)
	return err
}

func (s *Store) ListComments(ctx context.Context, postID string, limit int) ([]channels.Comment, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, post_id::text, author_id::text, body, created_at FROM channel_comments
		 WHERE post_id = $1 AND deleted_at IS NULL ORDER BY created_at LIMIT $2`, postID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []channels.Comment
	for rows.Next() {
		var c channels.Comment
		if err := rows.Scan(&c.ID, &c.PostID, &c.AuthorID, &c.Body, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) CommentCount(ctx context.Context, postID string) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM channel_comments WHERE post_id = $1 AND deleted_at IS NULL`, postID).Scan(&n)
	return n, err
}

func (s *Store) GetComment(ctx context.Context, id string) (channels.Comment, error) {
	var c channels.Comment
	err := s.pool.QueryRow(ctx,
		`SELECT id, post_id::text, author_id::text, body, created_at FROM channel_comments WHERE id = $1 AND deleted_at IS NULL`, id).
		Scan(&c.ID, &c.PostID, &c.AuthorID, &c.Body, &c.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return channels.Comment{}, channels.ErrNotFound
	}
	return c, err
}

func (s *Store) DeleteComment(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `UPDATE channel_comments SET deleted_at = now() WHERE id = $1 AND deleted_at IS NULL`, id)
	return err
}

func collectChannels(rows pgx.Rows) ([]channels.Channel, error) {
	defer rows.Close()
	var out []channels.Channel
	for rows.Next() {
		c, err := scanChannel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func collectPosts(rows pgx.Rows) ([]channels.Post, error) {
	defer rows.Close()
	var out []channels.Post
	for rows.Next() {
		p, err := scanPost(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
