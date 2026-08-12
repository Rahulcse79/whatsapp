package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/whatsapp-v2/server/internal/media"
)

// TenorProvider implements media.GifProvider over Tenor's v2 API. It is the ONLY
// party Tenor sees: the request is issued by media-svc with the server's key and
// carries no client IP or forwarded headers (FR-MED-05). The client key is a
// non-secret app identifier Tenor uses for analytics/abuse, not for auth.
type TenorProvider struct {
	apiKey    string
	clientKey string
	baseURL   string
	http      *http.Client
}

// NewTenorProvider builds a provider with the server's API key. baseURL is fixed
// to Tenor; NewTenorProviderWithBase overrides it for tests.
func NewTenorProvider(apiKey string) *TenorProvider {
	return NewTenorProviderWithBase(apiKey, "https://tenor.googleapis.com/v2")
}

func NewTenorProviderWithBase(apiKey, baseURL string) *TenorProvider {
	return &TenorProvider{
		apiKey:    apiKey,
		clientKey: "whatsapp-v2",
		baseURL:   baseURL,
		http:      &http.Client{Timeout: 5 * time.Second},
	}
}

type tenorResponse struct {
	Results []struct {
		ID           string `json:"id"`
		MediaFormats struct {
			Gif     tenorFormat `json:"gif"`
			TinyGif tenorFormat `json:"tinygif"`
		} `json:"media_formats"`
	} `json:"results"`
}

type tenorFormat struct {
	URL  string `json:"url"`
	Dims []int  `json:"dims"` // [width, height]
}

// Search proxies one query to Tenor and normalizes the hits. The outbound
// request deliberately sets no X-Forwarded-For / X-Real-IP / client headers —
// Tenor observes only this service, which is the entire privacy guarantee.
func (t *TenorProvider) Search(ctx context.Context, query string, limit int) ([]media.GifResult, error) {
	q := url.Values{}
	q.Set("q", query)
	q.Set("key", t.apiKey)
	q.Set("client_key", t.clientKey)
	q.Set("limit", strconv.Itoa(limit))
	q.Set("media_filter", "gif,tinygif")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.baseURL+"/search?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := t.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tenor: unexpected status %d", resp.StatusCode)
	}

	var parsed tenorResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}

	out := make([]media.GifResult, 0, len(parsed.Results))
	for _, r := range parsed.Results {
		full := r.MediaFormats.Gif
		preview := r.MediaFormats.TinyGif
		if full.URL == "" {
			continue
		}
		if preview.URL == "" {
			preview = full
		}
		w, h := 0, 0
		if len(full.Dims) == 2 {
			w, h = full.Dims[0], full.Dims[1]
		}
		out = append(out, media.GifResult{ID: r.ID, URL: full.URL, PreviewURL: preview.URL, Width: w, Height: h})
	}
	return out, nil
}

var _ media.GifProvider = (*TenorProvider)(nil)
