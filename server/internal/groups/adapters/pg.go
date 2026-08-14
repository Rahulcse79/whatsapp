package adapters

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/whatsapp-v2/server/internal/groups"
	"github.com/whatsapp-v2/server/internal/groups/domain"
)

// Store implements groups.Repo over one PostgreSQL pool. Version bumps and
// membership changes are done in a single transaction so the emitted event
// carries the committed version.
type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func (s *Store) CreateGroup(ctx context.Context, g groups.Group, members []groups.Member) error {
	settings, _ := json.Marshal(g.Settings)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx,
		`INSERT INTO groups (id, name, description, settings, version, created_by, created_at)
		 VALUES ($1, $2, $3, $4::jsonb, $5, $6, $7)`,
		g.ID, g.Name, g.Description, string(settings), g.Version, g.CreatedBy, g.CreatedAt); err != nil {
		return err
	}
	for _, m := range members {
		if _, err := tx.Exec(ctx,
			`INSERT INTO group_members (group_id, user_id, role, joined_at) VALUES ($1, $2, $3, $4)`,
			m.GroupID, m.UserID, int16(m.Role), m.JoinedAt); err != nil {
			return err
		}
	}
	// The group is also a conversation (id == group id, kind=1). The chat
	// accept/fan-out pipeline resolves recipients from conversation_members, so
	// group membership must be mirrored there or group messages deliver to
	// nobody. group_id FK is ON DELETE CASCADE, so deleting the group drops the
	// conversation row and its members.
	if _, err := tx.Exec(ctx,
		`INSERT INTO conversations (id, kind, group_id) VALUES ($1, 1, $1) ON CONFLICT (id) DO NOTHING`,
		g.ID); err != nil {
		return err
	}
	if err := mirrorMembers(ctx, tx, g.ID, membersUserIDs(members)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// mirrorMembers upserts the given users into conversation_members for convID —
// keeping the messaging fan-out membership in sync with group_members.
func mirrorMembers(ctx context.Context, tx pgx.Tx, convID string, userIDs []string) error {
	if len(userIDs) == 0 {
		return nil
	}
	_, err := tx.Exec(ctx,
		`INSERT INTO conversation_members (conversation_id, user_id)
		 SELECT $1, u::uuid FROM unnest($2::text[]) AS u
		 ON CONFLICT DO NOTHING`, convID, userIDs)
	return err
}

func membersUserIDs(members []groups.Member) []string {
	out := make([]string, len(members))
	for i, m := range members {
		out[i] = m.UserID
	}
	return out
}

func (s *Store) GetGroup(ctx context.Context, id string) (groups.Group, error) {
	var (
		g        groups.Group
		settings []byte
	)
	err := s.pool.QueryRow(ctx,
		`SELECT id, name, COALESCE(description,''), settings, version, COALESCE(created_by::text,''), created_at
		 FROM groups WHERE id = $1`, id).
		Scan(&g.ID, &g.Name, &g.Description, &settings, &g.Version, &g.CreatedBy, &g.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return groups.Group{}, groups.ErrNotFound
	}
	if err != nil {
		return groups.Group{}, err
	}
	_ = json.Unmarshal(settings, &g.Settings)
	return g, nil
}

func (s *Store) GetMember(ctx context.Context, gid, uid string) (groups.Member, error) {
	var (
		role int16
		m    = groups.Member{GroupID: gid, UserID: uid}
	)
	err := s.pool.QueryRow(ctx,
		`SELECT role, joined_at FROM group_members WHERE group_id = $1 AND user_id = $2`, gid, uid).
		Scan(&role, &m.JoinedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return groups.Member{}, groups.ErrNotFound
	}
	if err != nil {
		return groups.Member{}, err
	}
	m.Role = domain.Role(role)
	return m, nil
}

func (s *Store) ListMembers(ctx context.Context, gid, afterUserID string, limit int) ([]groups.Member, error) {
	var after any
	if afterUserID != "" {
		after = afterUserID
	}
	rows, err := s.pool.Query(ctx,
		`SELECT user_id::text, role, joined_at FROM group_members
		 WHERE group_id = $1 AND ($2::uuid IS NULL OR user_id > $2::uuid)
		 ORDER BY user_id LIMIT $3`, gid, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []groups.Member
	for rows.Next() {
		var (
			m    = groups.Member{GroupID: gid}
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

func (s *Store) CountMembers(ctx context.Context, gid string) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM group_members WHERE group_id = $1`, gid).Scan(&n)
	return n, err
}

// MembersAndVersion returns the full member set + the group version — the
// authoritative reload for the membership cache (groups.MemberSource).
func (s *Store) MembersAndVersion(ctx context.Context, gid string) ([]string, int64, error) {
	var version int64
	err := s.pool.QueryRow(ctx, `SELECT version FROM groups WHERE id = $1`, gid).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, 0, groups.ErrNotFound
	}
	if err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `SELECT user_id::text FROM group_members WHERE group_id = $1`, gid)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var members []string
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			return nil, 0, err
		}
		members = append(members, uid)
	}
	return members, version, rows.Err()
}

func (s *Store) UpdateInfo(ctx context.Context, gid string, name, description, avatarRef *string) (int64, error) {
	var version int64
	err := s.pool.QueryRow(ctx,
		`UPDATE groups SET name = COALESCE($2, name),
		                   description = COALESCE($3, description),
		                   avatar_ref = COALESCE($4::uuid, avatar_ref),
		                   version = version + 1
		 WHERE id = $1 RETURNING version`, gid, name, description, avatarRef).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, groups.ErrNotFound
	}
	return version, err
}

func (s *Store) UpdateSettings(ctx context.Context, gid string, st domain.Settings) (int64, error) {
	settings, _ := json.Marshal(st)
	var version int64
	err := s.pool.QueryRow(ctx,
		`UPDATE groups SET settings = $2::jsonb, version = version + 1 WHERE id = $1 RETURNING version`,
		gid, string(settings)).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, groups.ErrNotFound
	}
	return version, err
}

func (s *Store) AddMembers(ctx context.Context, gid string, userIDs []string) ([]string, int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx,
		`INSERT INTO group_members (group_id, user_id, role)
		 SELECT $1, u::uuid, 0 FROM unnest($2::text[]) AS u
		 ON CONFLICT (group_id, user_id) DO NOTHING
		 RETURNING user_id::text`, gid, userIDs)
	if err != nil {
		return nil, 0, err
	}
	var added []string
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			rows.Close()
			return nil, 0, err
		}
		added = append(added, uid)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	// Mirror into conversation_members so the added users receive group messages.
	if err := mirrorMembers(ctx, tx, gid, added); err != nil {
		return nil, 0, err
	}

	var version int64
	if err := tx.QueryRow(ctx, `UPDATE groups SET version = version + 1 WHERE id = $1 RETURNING version`, gid).Scan(&version); err != nil {
		return nil, 0, err
	}
	return added, version, tx.Commit(ctx)
}

func (s *Store) RemoveMember(ctx context.Context, gid, uid string) (int64, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx, `DELETE FROM group_members WHERE group_id = $1 AND user_id = $2`, gid, uid)
	if err != nil {
		return 0, false, err
	}
	if tag.RowsAffected() == 0 {
		return 0, false, tx.Commit(ctx)
	}
	// Drop the mirrored conversation membership so they stop receiving messages.
	if _, err := tx.Exec(ctx, `DELETE FROM conversation_members WHERE conversation_id = $1 AND user_id = $2`, gid, uid); err != nil {
		return 0, false, err
	}
	var version int64
	if err := tx.QueryRow(ctx, `UPDATE groups SET version = version + 1 WHERE id = $1 RETURNING version`, gid).Scan(&version); err != nil {
		return 0, false, err
	}
	return version, true, tx.Commit(ctx)
}

func (s *Store) SetRole(ctx context.Context, gid, uid string, role domain.Role) (int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx, `UPDATE group_members SET role = $3 WHERE group_id = $1 AND user_id = $2`, gid, uid, int16(role))
	if err != nil {
		return 0, err
	}
	if tag.RowsAffected() == 0 {
		return 0, groups.ErrNotFound
	}
	var version int64
	if err := tx.QueryRow(ctx, `UPDATE groups SET version = version + 1 WHERE id = $1 RETURNING version`, gid).Scan(&version); err != nil {
		return 0, err
	}
	return version, tx.Commit(ctx)
}

func (s *Store) DeleteGroup(ctx context.Context, gid string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM groups WHERE id = $1`, gid)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return groups.ErrNotFound
	}
	return nil
}

func (s *Store) CreateInvite(ctx context.Context, l groups.InviteLink) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO invite_links (token, group_id, created_by, expires_at, max_uses, uses)
		 VALUES ($1, $2, $3, $4, $5, 0)`,
		l.Token, l.GroupID, l.CreatedBy, l.ExpiresAt, l.MaxUses)
	return err
}

func (s *Store) GetInvite(ctx context.Context, token string) (groups.InviteLink, error) {
	var l groups.InviteLink
	err := s.pool.QueryRow(ctx,
		`SELECT token, group_id::text, COALESCE(created_by::text,''), expires_at, revoked_at, max_uses, uses
		 FROM invite_links WHERE token = $1`, token).
		Scan(&l.Token, &l.GroupID, &l.CreatedBy, &l.ExpiresAt, &l.RevokedAt, &l.MaxUses, &l.Uses)
	if errors.Is(err, pgx.ErrNoRows) {
		return groups.InviteLink{}, groups.ErrNotFound
	}
	return l, err
}

func (s *Store) RevokeInvite(ctx context.Context, token string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE invite_links SET revoked_at = now() WHERE token = $1`, token)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return groups.ErrNotFound
	}
	return nil
}

func (s *Store) JoinViaInvite(ctx context.Context, token, userID string, maxMembers int) (groups.Group, int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return groups.Group{}, 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var (
		gid     string
		expires *time.Time
		revoked *time.Time
		maxUses *int
		uses    int
	)
	err = tx.QueryRow(ctx,
		`SELECT group_id::text, expires_at, revoked_at, max_uses, uses
		 FROM invite_links WHERE token = $1 FOR UPDATE`, token).
		Scan(&gid, &expires, &revoked, &maxUses, &uses)
	if errors.Is(err, pgx.ErrNoRows) {
		return groups.Group{}, 0, groups.ErrInviteInvalid
	}
	if err != nil {
		return groups.Group{}, 0, err
	}
	if revoked != nil ||
		(expires != nil && time.Now().After(*expires)) ||
		(maxUses != nil && uses >= *maxUses) {
		return groups.Group{}, 0, groups.ErrInviteInvalid
	}

	var (
		g        = groups.Group{ID: gid}
		settings []byte
	)
	err = tx.QueryRow(ctx,
		`SELECT name, COALESCE(description,''), settings, version FROM groups WHERE id = $1`, gid).
		Scan(&g.Name, &g.Description, &settings, &g.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		return groups.Group{}, 0, groups.ErrInviteInvalid
	}
	if err != nil {
		return groups.Group{}, 0, err
	}
	_ = json.Unmarshal(settings, &g.Settings)

	var alreadyMember bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM group_members WHERE group_id = $1 AND user_id = $2)`, gid, userID).
		Scan(&alreadyMember); err != nil {
		return groups.Group{}, 0, err
	}
	if alreadyMember {
		return g, g.Version, groups.ErrAlreadyMember
	}

	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM group_members WHERE group_id = $1`, gid).Scan(&count); err != nil {
		return groups.Group{}, 0, err
	}
	if count >= maxMembers {
		return g, 0, groups.ErrGroupFull
	}

	if _, err := tx.Exec(ctx, `INSERT INTO group_members (group_id, user_id, role) VALUES ($1, $2, 0)`, gid, userID); err != nil {
		return groups.Group{}, 0, err
	}
	if err := mirrorMembers(ctx, tx, gid, []string{userID}); err != nil {
		return groups.Group{}, 0, err
	}
	if _, err := tx.Exec(ctx, `UPDATE invite_links SET uses = uses + 1 WHERE token = $1`, token); err != nil {
		return groups.Group{}, 0, err
	}
	var version int64
	if err := tx.QueryRow(ctx, `UPDATE groups SET version = version + 1 WHERE id = $1 RETURNING version`, gid).Scan(&version); err != nil {
		return groups.Group{}, 0, err
	}
	g.Version = version
	return g, version, tx.Commit(ctx)
}
