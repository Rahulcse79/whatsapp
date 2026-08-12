package media

import (
	"context"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
)

// GIF search proxy (FR-MED-05). The point is privacy: the client never talks to
// the GIF provider, so the provider never sees a user's IP or headers — media-svc
// is the only caller it observes. Disabled in the air-gap (offline) profile,
// where no external provider is reachable and stickers are the local fallback.

// GifResult is one provider-agnostic GIF hit (normalized across Tenor/Giphy).
type GifResult struct {
	ID         string `json:"id"`
	URL        string `json:"url"`         // full-size animated asset
	PreviewURL string `json:"preview_url"` // small preview for the picker grid
	Width      int    `json:"width"`
	Height     int    `json:"height"`
}

// GifProvider is the upstream search API. Implementations MUST NOT forward the
// client's IP/headers — the whole feature exists to keep the client's address
// away from the provider (FR-MED-05).
type GifProvider interface {
	Search(ctx context.Context, query string, limit int) ([]GifResult, error)
}

const (
	gifMaxQueryRunes = 100
	gifDefaultLimit  = 24
	gifMaxLimit      = 50
)

// GifService proxies GIF search server-side, with per-user rate limiting. A nil
// provider means the feature is disabled (air-gap profile / no key configured);
// Search then returns FEATURE_DISABLED rather than pretending to work.
type GifService struct {
	provider GifProvider
	rate     Rate
	limit    int
}

func NewGifService(provider GifProvider, rate Rate) *GifService {
	return &GifService{provider: provider, rate: rate, limit: gifDefaultLimit}
}

// Enabled reports whether a provider is configured (false in the air-gap profile).
func (g *GifService) Enabled() bool { return g.provider != nil }

// Search validates + rate-limits the query, then proxies it to the provider.
// limit ≤ 0 (or over the cap) falls back to the default.
func (g *GifService) Search(ctx context.Context, ident auth.Identity, query string, limit int) ([]GifResult, error) {
	if g.provider == nil {
		return nil, httpx.Reject(http.StatusNotFound, "FEATURE_DISABLED", "gif search is disabled in this deployment")
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, httpx.Reject(http.StatusBadRequest, "VALIDATION_QUERY", "q is required")
	}
	if utf8.RuneCountInString(query) > gifMaxQueryRunes {
		return nil, httpx.Reject(http.StatusBadRequest, "VALIDATION_QUERY", "q is too long")
	}
	if limit <= 0 || limit > gifMaxLimit {
		limit = g.limit
	}
	if g.rate != nil {
		if ok, err := g.rate.Allow(ctx, "gif-search:"+ident.UserID); err != nil {
			return nil, httpx.Transient()
		} else if !ok {
			return nil, httpx.Reject(http.StatusTooManyRequests, "RATE_LIMITED", "too many searches, slow down")
		}
	}
	results, err := g.provider.Search(ctx, query, limit)
	if err != nil {
		// The provider is upstream of us; a failure there is a bad gateway, not
		// our fault — and never leaks provider internals to the client.
		return nil, httpx.Reject(http.StatusBadGateway, "PROVIDER_UNAVAILABLE", "gif provider unavailable")
	}
	return results, nil
}
