package flags

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

// ValkeyCache is the shared 30 s rule cache (core-api-lld §5). Entries — present
// and absent alike — are JSON blobs under flags:{flag} with a CacheTTL expiry,
// so all pods converge within the window and a write can bust a key outright.
type ValkeyCache struct {
	client *redis.Client
	ttl    time.Duration
}

func NewValkeyCache(client *redis.Client) *ValkeyCache {
	return &ValkeyCache{client: client, ttl: CacheTTL}
}

var _ Cache = (*ValkeyCache)(nil)

func cacheKey(flag string) string { return "flags:" + flag }

func (c *ValkeyCache) Get(ctx context.Context, flag string) (Entry, bool, error) {
	b, err := c.client.Get(ctx, cacheKey(flag)).Bytes()
	if errors.Is(err, redis.Nil) {
		return Entry{}, false, nil
	}
	if err != nil {
		return Entry{}, false, err
	}
	var e Entry
	if err := json.Unmarshal(b, &e); err != nil {
		return Entry{}, false, err
	}
	return e, true, nil
}

func (c *ValkeyCache) Put(ctx context.Context, flag string, e Entry) error {
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	return c.client.Set(ctx, cacheKey(flag), b, c.ttl).Err()
}

func (c *ValkeyCache) Del(ctx context.Context, flag string) error {
	return c.client.Del(ctx, cacheKey(flag)).Err()
}
