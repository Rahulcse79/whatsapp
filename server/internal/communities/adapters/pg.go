// Package adapters implements communities.Store over PostgreSQL (communities +
// community_members + community_groups + community_events; migration 000021).
package adapters

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/whatsapp-v2/server/internal/communities"
	"github.com/whatsapp-v2/server/internal/communities/domain"
)

type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func (s *Store) Create(ctx context.Context, c communities.Community) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO communities (id, name, description, kind, owner_id, announcement_group_id, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		c.ID, c.Name, c.Description, int16(c.Kind), c.OwnerID, c.AnnouncementGroupID, c.CreatedAt)
	return err
}

func (s *Store) Get(ctx context.Context, id string) (communities.Community, error) {
	var c communities.Community
	var kind int16
	err := s.pool.QueryRow(ctx,
		`SELECT id, name, description, kind, owner_id, announcement_group_id, created_at
		 FROM communities WHERE id = $1`, id).
		Scan(&c.ID, &c.Name, &c.Description, &kind, &c.OwnerID, &c.AnnouncementGroupID, &c.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return communities.Community{}, communities.ErrNotFound
	}
	c.Kind = domain.Kind(kind)
	return c, err
}

func (s *Store) Delete(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM communities WHERE id = $1`, id)
	return err
}

func (s *Store) Counts(ctx context.Context, id string) (int, int, error) {
	var members, groups int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM community_members WHERE community_id = $1`, id).Scan(&members); err != nil {
		return 0, 0, err
	}
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM community_groups WHERE community_id = $1`, id).Scan(&groups); err != nil {
		return 0, 0, err
	}
	return members, groups, nil
}

func (s *Store) AddMember(ctx context.Context, communityID, userID string, role domain.Role) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO community_members (community_id, user_id, role) VALUES ($1, $2, $3)
		 ON CONFLICT (community_id, user_id) DO NOTHING`,
		communityID, userID, int16(role))
	return err
}

func (s *Store) RemoveMember(ctx context.Context, communityID, userID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM community_members WHERE community_id = $1 AND user_id = $2`, communityID, userID)
	return err
}

func (s *Store) GetMember(ctx context.Context, communityID, userID string) (communities.Member, error) {
	var m communities.Member
	var role int16
	err := s.pool.QueryRow(ctx,
		`SELECT user_id, role FROM community_members WHERE community_id = $1 AND user_id = $2`,
		communityID, userID).Scan(&m.UserID, &role)
	if errors.Is(err, pgx.ErrNoRows) {
		return communities.Member{}, communities.ErrNotFound
	}
	m.Role = domain.Role(role)
	return m, err
}

func (s *Store) ListMembers(ctx context.Context, communityID string) ([]communities.Member, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT user_id, role FROM community_members WHERE community_id = $1 ORDER BY role DESC, joined_at`, communityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []communities.Member
	for rows.Next() {
		var m communities.Member
		var role int16
		if err := rows.Scan(&m.UserID, &role); err != nil {
			return nil, err
		}
		m.Role = domain.Role(role)
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) SetRole(ctx context.Context, communityID, userID string, role domain.Role) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE community_members SET role = $3 WHERE community_id = $1 AND user_id = $2`,
		communityID, userID, int16(role))
	return err
}

func (s *Store) AddGroup(ctx context.Context, communityID, groupID string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO community_groups (community_id, group_id) VALUES ($1, $2)
		 ON CONFLICT (community_id, group_id) DO NOTHING`, communityID, groupID)
	return err
}

func (s *Store) RemoveGroup(ctx context.Context, communityID, groupID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM community_groups WHERE community_id = $1 AND group_id = $2`, communityID, groupID)
	return err
}

func (s *Store) ListGroups(ctx context.Context, communityID string) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT group_id FROM community_groups WHERE community_id = $1 ORDER BY added_at`, communityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var g string
		if err := rows.Scan(&g); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (s *Store) CreateEvent(ctx context.Context, e communities.Event) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO community_events (id, community_id, title, description, starts_at, created_by, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		e.ID, e.CommunityID, e.Title, e.Description, e.StartsAt, e.CreatedBy, e.CreatedAt)
	return err
}

func (s *Store) ListEvents(ctx context.Context, communityID string, from time.Time) ([]communities.Event, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, community_id, title, description, starts_at, created_by, created_at
		 FROM community_events WHERE community_id = $1 AND starts_at >= $2 ORDER BY starts_at`, communityID, from)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []communities.Event
	for rows.Next() {
		var e communities.Event
		if err := rows.Scan(&e.ID, &e.CommunityID, &e.Title, &e.Description, &e.StartsAt, &e.CreatedBy, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) DeleteEvent(ctx context.Context, communityID, eventID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM community_events WHERE community_id = $1 AND id = $2`, communityID, eventID)
	return err
}

const summarySelect = `
	SELECT c.id, c.name, c.description, count(m.user_id) AS members
	FROM communities c LEFT JOIN community_members m ON m.community_id = c.id
	WHERE c.kind = 0`

func (s *Store) Discover(ctx context.Context, limit int) ([]communities.Summary, error) {
	return s.scanSummaries(ctx,
		summarySelect+` GROUP BY c.id ORDER BY members DESC, c.created_at DESC LIMIT $1`, limit)
}

func (s *Store) Search(ctx context.Context, query string, limit int) ([]communities.Summary, error) {
	return s.scanSummaries(ctx,
		summarySelect+` AND c.name ILIKE '%' || $1 || '%' GROUP BY c.id ORDER BY members DESC LIMIT $2`, query, limit)
}

func (s *Store) scanSummaries(ctx context.Context, sql string, args ...any) ([]communities.Summary, error) {
	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []communities.Summary{}
	for rows.Next() {
		var sm communities.Summary
		if err := rows.Scan(&sm.ID, &sm.Name, &sm.Description, &sm.MemberCount); err != nil {
			return nil, err
		}
		out = append(out, sm)
	}
	return out, rows.Err()
}
