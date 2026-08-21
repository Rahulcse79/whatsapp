// Package adapters implements the discovery search over PostgreSQL — the default
// backend (source of truth) that queries the PUBLIC source tables (channels,
// communities, users) via the existing trigram/ILIKE indexes. An OpenSearch
// adapter would implement the same discovery.SearchBackend and be fed by the
// reindex pipeline; PGSource + NoopIndexer support that path. Metadata only.
package adapters

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/whatsapp-v2/server/internal/discovery"
	"github.com/whatsapp-v2/server/internal/discovery/domain"
)

// ── search backend ───────────────────────────────────────────────────────────

type Backend struct{ pool *pgxpool.Pool }

func NewBackend(pool *pgxpool.Pool) *Backend { return &Backend{pool: pool} }

// likePattern escapes LIKE metacharacters and wraps the term for a contains match.
func likePattern(q string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(q) + "%"
}

func wants(kinds []domain.Kind, k domain.Kind) bool {
	if len(kinds) == 0 {
		return true
	}
	for _, x := range kinds {
		if x == k {
			return true
		}
	}
	return false
}

func (b *Backend) Search(ctx context.Context, query string, kinds []domain.Kind, limit int) ([]discovery.Result, error) {
	pat := likePattern(query)
	var out []discovery.Result

	if wants(kinds, domain.KindChannel) {
		rows, err := b.pool.Query(ctx,
			`SELECT id, name, handle, description, verified FROM channels
			 WHERE kind = 0 AND deleted_at IS NULL
			   AND (name ILIKE $1 ESCAPE '\' OR handle ILIKE $1 ESCAPE '\' OR description ILIKE $1 ESCAPE '\')
			 LIMIT $2`, pat, limit)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id, name, handle, desc string
			var verified bool
			if err := rows.Scan(&id, &name, &handle, &desc, &verified); err != nil {
				rows.Close()
				return nil, err
			}
			out = append(out, discovery.Result{Kind: domain.KindChannel, ID: id, Title: name, Subtitle: desc, Handle: "@" + handle, Verified: verified})
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}

	if wants(kinds, domain.KindCommunity) {
		rows, err := b.pool.Query(ctx,
			`SELECT id, name, description FROM communities
			 WHERE kind = 0 AND (name ILIKE $1 ESCAPE '\' OR description ILIKE $1 ESCAPE '\')
			 LIMIT $2`, pat, limit)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id, name, desc string
			if err := rows.Scan(&id, &name, &desc); err != nil {
				rows.Close()
				return nil, err
			}
			out = append(out, discovery.Result{Kind: domain.KindCommunity, ID: id, Title: name, Subtitle: desc})
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}

	if wants(kinds, domain.KindUser) {
		rows, err := b.pool.Query(ctx,
			`SELECT id, username::text, COALESCE(display_name, '') FROM users
			 WHERE username IS NOT NULL AND username::text ILIKE $1 ESCAPE '\'
			 LIMIT $2`, pat, limit)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id, username, display string
			if err := rows.Scan(&id, &username, &display); err != nil {
				rows.Close()
				return nil, err
			}
			out = append(out, discovery.Result{Kind: domain.KindUser, ID: id, Title: username, Subtitle: display, Handle: "@" + username})
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// ── reindex source (feeds an external index) ─────────────────────────────────

type Source struct{ pool *pgxpool.Pool }

func NewSource(pool *pgxpool.Pool) *Source { return &Source{pool: pool} }

func (s *Source) AllDocs(ctx context.Context) ([]discovery.Doc, error) {
	var out []discovery.Doc
	// public channels
	rows, err := s.pool.Query(ctx, `SELECT id, name, handle, description, verified FROM channels WHERE kind = 0 AND deleted_at IS NULL`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id, name, handle, desc string
		var verified bool
		if err := rows.Scan(&id, &name, &handle, &desc, &verified); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, discovery.Doc{Kind: domain.KindChannel, ID: id, Title: name, Handle: handle, Description: desc, Verified: verified})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// public communities
	rows, err = s.pool.Query(ctx, `SELECT id, name, description FROM communities WHERE kind = 0`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id, name, desc string
		if err := rows.Scan(&id, &name, &desc); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, discovery.Doc{Kind: domain.KindCommunity, ID: id, Title: name, Description: desc})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// usernames
	rows, err = s.pool.Query(ctx, `SELECT id, username::text, COALESCE(display_name, '') FROM users WHERE username IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id, username, display string
		if err := rows.Scan(&id, &username, &display); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, discovery.Doc{Kind: domain.KindUser, ID: id, Title: username, Handle: username, Description: display})
	}
	rows.Close()
	return out, rows.Err()
}

// NoopIndexer is the default sink: the Postgres backend IS the index, so there's
// nothing to push. The OpenSearch adapter replaces this to keep its index fresh.
type NoopIndexer struct{}

func (NoopIndexer) Index(context.Context, discovery.Doc) error        { return nil }
func (NoopIndexer) Delete(context.Context, domain.Kind, string) error { return nil }
