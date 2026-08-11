package adapters

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

// ValkeyMembershipCache caches versioned group member sets:
// group_members:{id} (set) + group_ver:{id} (the cached set's version).
// data-structures-algorithms.md §12. TTL-bounded so stale entries self-expire.
type ValkeyMembershipCache struct {
	client *redis.Client
	ttl    time.Duration
}

func NewValkeyMembershipCache(client *redis.Client) *ValkeyMembershipCache {
	return &ValkeyMembershipCache{client: client, ttl: 15 * time.Minute}
}

func membersKey(gid string) string { return "group_members:" + gid }
func verKey(gid string) string     { return "group_ver:" + gid }

func (c *ValkeyMembershipCache) Get(ctx context.Context, gid string) ([]string, int64, bool, error) {
	version, err := c.client.Get(ctx, verKey(gid)).Int64()
	if errors.Is(err, redis.Nil) {
		return nil, 0, false, nil
	}
	if err != nil {
		return nil, 0, false, err
	}
	members, err := c.client.SMembers(ctx, membersKey(gid)).Result()
	if err != nil {
		return nil, 0, false, err
	}
	return members, version, true, nil
}

func (c *ValkeyMembershipCache) Put(ctx context.Context, gid string, members []string, version int64) error {
	pipe := c.client.TxPipeline()
	pipe.Del(ctx, membersKey(gid))
	if len(members) > 0 {
		vals := make([]interface{}, len(members))
		for i, m := range members {
			vals[i] = m
		}
		pipe.SAdd(ctx, membersKey(gid), vals...)
		pipe.Expire(ctx, membersKey(gid), c.ttl)
	}
	pipe.Set(ctx, verKey(gid), version, c.ttl)
	_, err := pipe.Exec(ctx)
	return err
}

func (c *ValkeyMembershipCache) Invalidate(ctx context.Context, gid string) error {
	return c.client.Del(ctx, membersKey(gid), verKey(gid)).Err()
}
