// Package adapters implements the gateway's RouteStore over Valkey. Route
// ownership is enforced with atomic Lua so a stale pod can neither refresh
// nor release a route another pod has taken over (ws-gateway-lld §3).
package adapters

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// ValkeyRouteStore implements gateway.RouteStore.
type ValkeyRouteStore struct {
	client  *redis.Client
	refresh *redis.Script
	release *redis.Script
}

func NewValkeyRouteStore(client *redis.Client) *ValkeyRouteStore {
	return &ValkeyRouteStore{
		client: client,
		// Extend TTL only if we still own the route.
		refresh: redis.NewScript(`
			if redis.call('GET', KEYS[1]) == ARGV[1] then
			  redis.call('PEXPIRE', KEYS[1], ARGV[2])
			  return 1
			end
			return 0`),
		// Delete only if we still own the route.
		release: redis.NewScript(`
			if redis.call('GET', KEYS[1]) == ARGV[1] then
			  redis.call('DEL', KEYS[1])
			end
			return 1`),
	}
}

func key(deviceID string) string { return "route:" + deviceID }

// Claim sets the route unconditionally: the newest connection owns the device
// (last-writer-wins).
func (s *ValkeyRouteStore) Claim(ctx context.Context, deviceID, podID string, ttl time.Duration) error {
	if err := s.client.Set(ctx, key(deviceID), podID, ttl).Err(); err != nil {
		return fmt.Errorf("route claim: %w", err)
	}
	return nil
}

// Refresh extends the TTL iff podID still owns the route.
func (s *ValkeyRouteStore) Refresh(ctx context.Context, deviceID, podID string, ttl time.Duration) (bool, error) {
	v, err := s.refresh.Run(ctx, s.client, []string{key(deviceID)}, podID, ttl.Milliseconds()).Int()
	if err != nil {
		return false, fmt.Errorf("route refresh: %w", err)
	}
	return v == 1, nil
}

// Release deletes the route iff podID still owns it.
func (s *ValkeyRouteStore) Release(ctx context.Context, deviceID, podID string) error {
	if err := s.release.Run(ctx, s.client, []string{key(deviceID)}, podID).Err(); err != nil {
		return fmt.Errorf("route release: %w", err)
	}
	return nil
}
