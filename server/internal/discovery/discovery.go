// Package discovery is the public metadata search + discovery plane (T13.01): a
// unified search over channels, PUBLIC communities, and usernames. It indexes
// METADATA ONLY — names, handles, descriptions, usernames — never E2EE message
// content. The search backend is pluggable: a Postgres trigram adapter is the
// default (source of truth), and an OpenSearch/Elasticsearch adapter is a drop-in
// (fed by the reindex/sync pipeline). All results are public — nothing gated is
// ever surfaced.
package discovery

import (
	"context"

	"github.com/whatsapp-v2/server/internal/discovery/domain"
)

// Result is one search hit.
type Result struct {
	Kind     domain.Kind
	ID       string
	Title    string
	Subtitle string
	Handle   string
	Verified bool
	Score    float64
}

// ResultView is a hit over the wire.
type ResultView struct {
	Kind     string  `json:"kind"`
	ID       string  `json:"id"`
	Title    string  `json:"title"`
	Subtitle string  `json:"subtitle,omitempty"`
	Handle   string  `json:"handle,omitempty"`
	Verified bool    `json:"verified,omitempty"`
	Score    float64 `json:"score"`
}

// Doc is an indexable public document (what the sync pipeline pushes to an
// external index). Metadata only.
type Doc struct {
	Kind        domain.Kind
	ID          string
	Title       string
	Handle      string
	Description string
	Verified    bool
}

// SearchBackend returns candidate results for a query (already normalised),
// filtered to the requested kinds (empty = all). The Postgres adapter queries the
// public source tables; an OpenSearch adapter would query the index.
type SearchBackend interface {
	Search(ctx context.Context, query string, kinds []domain.Kind, limit int) ([]Result, error)
}

// Indexer is the sync-pipeline sink for an external index (OpenSearch). The
// default Postgres backend is the source of truth, so its Indexer is a no-op.
type Indexer interface {
	Index(ctx context.Context, d Doc) error
	Delete(ctx context.Context, kind domain.Kind, id string) error
}

// Source enumerates every public, indexable document — the full-reindex feed.
type Source interface {
	AllDocs(ctx context.Context) ([]Doc, error)
}
