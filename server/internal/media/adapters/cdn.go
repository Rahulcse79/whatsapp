package adapters

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/whatsapp-v2/server/internal/media"
	"github.com/whatsapp-v2/server/internal/media/domain"
)

// CDNDelivery mints edge URLs instead of MinIO presigned GETs (T15.04).
//
// The edge validates the token this mints (see deploy/compose/config/cdn/ for a
// reference nginx configuration) and, on a miss, pulls from MinIO over the
// private network. Because media blobs are E2EE ciphertext and immutable once
// complete, they are ideal cache objects: the edge can hold them for a long
// time and never learns anything from them.
//
// The token TTL and the cache lifetime are deliberately different concerns —
// the token bounds how long a *client* may start a fetch, while the edge's own
// cache-control bounds how long it keeps the bytes.
type CDNDelivery struct {
	baseURL string
	secret  []byte
}

// ErrCDNNotConfigured is returned by NewCDNDelivery when either half of the
// configuration is missing — a base URL without a key (or vice versa) would
// silently mint URLs the edge rejects, so it fails loudly at construction.
var ErrCDNNotConfigured = errors.New("media: CDN base URL and signing key must both be set")

func NewCDNDelivery(baseURL, signingKey string) (*CDNDelivery, error) {
	baseURL, signingKey = strings.TrimSpace(baseURL), strings.TrimSpace(signingKey)
	if baseURL == "" || signingKey == "" {
		return nil, ErrCDNNotConfigured
	}
	// Validate the base once here rather than per request.
	if _, err := domain.BuildCDNURL(baseURL, "probe", []byte(signingKey), 0); err != nil {
		return nil, err
	}
	return &CDNDelivery{baseURL: baseURL, secret: []byte(signingKey)}, nil
}

func (c *CDNDelivery) DownloadURL(_ context.Context, key string, expires time.Duration) (string, error) {
	return domain.BuildCDNURL(c.baseURL, key, c.secret, time.Now().Add(expires).Unix())
}

// Cacheable: an edge fronts these URLs, so a client may reuse one for its TTL.
func (c *CDNDelivery) Cacheable() bool { return true }

var _ media.Delivery = (*CDNDelivery)(nil)
