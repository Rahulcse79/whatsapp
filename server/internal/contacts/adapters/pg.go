// Package adapters holds the contacts context's I/O implementations: a single
// PostgreSQL Store implementing the discovery/edge/favorite/invite ports, and
// the GCRA-backed rate limiters (rate.go).
package adapters

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/whatsapp-v2/server/internal/contacts"
	"github.com/whatsapp-v2/server/internal/contacts/domain"
)

// Store implements the contacts context's PG-backed ports over one pool:
// Directory (discovery + username search against the users registry),
// ContactStore (owner→matched-user edges — peppered hashes only), Favorites,
// and Invites.
type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

var (
	_ contacts.Directory    = (*Store)(nil)
	_ contacts.ContactStore = (*Store)(nil)
	_ contacts.Favorites    = (*Store)(nil)
	_ contacts.Invites      = (*Store)(nil)
)

// statusActive is users.status = 0. Suspended (1) and tombstoned (2) users are
// neither discoverable by hash nor returned by search.
const statusActive = 0

// MatchHashes returns a Match for every peppered hash owned by an active user.
// The returned Hash is the exact bytes queried, so the caller can map a hit back
// to its address-book handle. Only hits are returned (order/subset unspecified).
func (s *Store) MatchHashes(ctx context.Context, hashes [][]byte) ([]contacts.Match, error) {
	if len(hashes) == 0 {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx,
		`SELECT phone_hash, id::text, COALESCE(username::text, '')
		   FROM users
		  WHERE phone_hash = ANY($1::bytea[]) AND status = $2`,
		hashes, statusActive)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []contacts.Match
	for rows.Next() {
		var m contacts.Match
		if err := rows.Scan(&m.Hash, &m.UserID, &m.Username); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// SearchUsername returns up to limit active users whose username matches query,
// trigram-ranked (the users_username_trgm GIN index assists the ILIKE). LIKE
// metacharacters in query are escaped so a caller can't widen the match.
func (s *Store) SearchUsername(ctx context.Context, query string, limit int) ([]contacts.UserRef, error) {
	pattern := "%" + escapeLike(query) + "%"
	rows, err := s.pool.Query(ctx,
		`SELECT id::text, username::text
		   FROM users
		  WHERE status = $3
		    AND username IS NOT NULL
		    AND username::text ILIKE $1 ESCAPE '\'
		  ORDER BY similarity(username::text, $2) DESC, username
		  LIMIT $4`,
		pattern, query, statusActive, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []contacts.UserRef
	for rows.Next() {
		var u contacts.UserRef
		if err := rows.Scan(&u.UserID, &u.Username); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// escapeLike neutralises LIKE/ILIKE metacharacters (\ % _) so a query is matched
// literally under `ESCAPE '\'`. NewReplacer substitutes simultaneously, so the
// backslash rule does not re-process the % / _ escapes it introduces.
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// UpsertMatched persists an owner's discovered edges (peppered hash → user).
// Re-syncing refreshes matched_user_id; plaintext handles are never stored.
func (s *Store) UpsertMatched(ctx context.Context, ownerID string, edges []contacts.ContactEdge) error {
	if len(edges) == 0 {
		return nil
	}
	hashes := make([][]byte, len(edges))
	users := make([]string, len(edges))
	for i, e := range edges {
		hashes[i] = e.Hash
		users[i] = e.MatchedUserID
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO contacts (owner_id, contact_phone_hash, matched_user_id)
		 SELECT $1, t.h, t.u::uuid
		   FROM unnest($2::bytea[], $3::text[]) AS t(h, u)
		 ON CONFLICT (owner_id, contact_phone_hash)
		 DO UPDATE SET matched_user_id = EXCLUDED.matched_user_id`,
		ownerID, hashes, users)
	return err
}

// Add marks target as a favorite of owner (idempotent). A target that is not a
// real user violates the FK and surfaces as contacts.ErrNotFound.
func (s *Store) Add(ctx context.Context, ownerID, targetID string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO favorites (owner_id, target_user_id) VALUES ($1, $2)
		 ON CONFLICT DO NOTHING`, ownerID, targetID)
	if isForeignKeyViolation(err) {
		return contacts.ErrNotFound
	}
	return err
}

// Remove unfavorites target (idempotent — removing a non-favorite is not an error).
func (s *Store) Remove(ctx context.Context, ownerID, targetID string) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM favorites WHERE owner_id = $1 AND target_user_id = $2`, ownerID, targetID)
	return err
}

// List returns owner's favorites joined to their (metadata-only) usernames.
func (s *Store) List(ctx context.Context, ownerID string) ([]contacts.UserRef, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT f.target_user_id::text, COALESCE(u.username::text, '')
		   FROM favorites f JOIN users u ON u.id = f.target_user_id
		  WHERE f.owner_id = $1
		  ORDER BY u.username`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []contacts.UserRef
	for rows.Next() {
		var u contacts.UserRef
		if err := rows.Scan(&u.UserID, &u.Username); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// Create persists an invite capability token.
func (s *Store) Create(ctx context.Context, inv domain.Invite) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO contact_invites (token, inviter_id, expires_at, max_uses, uses)
		 VALUES ($1, $2, $3, $4, $5)`,
		inv.Token, inv.InviterID, inv.ExpiresAt, inv.MaxUses, inv.Uses)
	return err
}

// Get loads an invite by token (contacts.ErrNotFound when unknown).
func (s *Store) Get(ctx context.Context, token string) (domain.Invite, error) {
	var inv domain.Invite
	err := s.pool.QueryRow(ctx,
		`SELECT token, inviter_id::text, expires_at, revoked_at, max_uses, uses
		   FROM contact_invites WHERE token = $1`, token).
		Scan(&inv.Token, &inv.InviterID, &inv.ExpiresAt, &inv.RevokedAt, &inv.MaxUses, &inv.Uses)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Invite{}, contacts.ErrNotFound
	}
	return inv, err
}

// Revoke revokes one of inviter's invites. Unknown token or a token owned by
// someone else → contacts.ErrNotFound. Re-revoking one's own token is a no-op
// success (the WHERE still matches the row).
func (s *Store) Revoke(ctx context.Context, inviterID, token string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE contact_invites SET revoked_at = now()
		  WHERE token = $1 AND inviter_id = $2`, token, inviterID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return contacts.ErrNotFound
	}
	return nil
}

// isForeignKeyViolation reports whether err is a PostgreSQL foreign-key
// violation (SQLSTATE 23503).
func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}
