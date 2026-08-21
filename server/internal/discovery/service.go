package discovery

import (
	"context"
	"net/http"
	"sort"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/discovery/domain"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
)

const (
	defaultLimit = 20
	maxLimit     = 50
	candidateCap = 200 // per-backend candidates to fetch before re-ranking
)

// Service runs the public discovery search + the reindex sync pipeline.
type Service struct {
	backend SearchBackend
	source  Source
	indexer Indexer
}

func NewService(backend SearchBackend, source Source, indexer Indexer) *Service {
	return &Service{backend: backend, source: source, indexer: indexer}
}

// Search returns public metadata hits ranked uniformly across entity types. Any
// authenticated user may search — every result is public. `kinds` (empty = all)
// filters to channels/communities/users.
func (s *Service) Search(ctx context.Context, _ auth.Identity, query string, kinds []domain.Kind, limit int) ([]ResultView, error) {
	if err := domain.ValidateQuery(query); err != nil {
		return nil, httpx.Reject(http.StatusBadRequest, "VALIDATION_QUERY", err.Error())
	}
	if limit <= 0 || limit > maxLimit {
		limit = defaultLimit
	}
	q := domain.NormalizeQuery(query)

	results, err := s.backend.Search(ctx, q, kinds, candidateCap)
	if err != nil {
		return nil, httpx.Transient()
	}
	// Re-score uniformly across types + sort (the backend may pre-filter but the
	// cross-type ordering is decided here).
	for i := range results {
		results[i].Score = domain.MatchScore(q, results[i].Title, results[i].Handle+" "+results[i].Subtitle, results[i].Verified)
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		return results[i].Title < results[j].Title
	})
	if len(results) > limit {
		results = results[:limit]
	}
	out := make([]ResultView, 0, len(results))
	for _, r := range results {
		if r.Score <= 0 {
			continue
		}
		out = append(out, ResultView{
			Kind: string(r.Kind), ID: r.ID, Title: r.Title, Subtitle: r.Subtitle,
			Handle: r.Handle, Verified: r.Verified, Score: r.Score,
		})
	}
	return out, nil
}

// Reindex is the sync pipeline: it reads every public doc from the Source and
// pushes it to the Indexer (an external OpenSearch index). With the Postgres
// backend the Indexer is a no-op, so this is a cheap sweep; wired to a periodic
// ticker in core-api. Incremental per-entity updates over NATS are a documented
// seam. Returns the number of docs indexed.
func (s *Service) Reindex(ctx context.Context) (int, error) {
	docs, err := s.source.AllDocs(ctx)
	if err != nil {
		return 0, err
	}
	for _, d := range docs {
		if err := s.indexer.Index(ctx, d); err != nil {
			return 0, err
		}
	}
	return len(docs), nil
}
