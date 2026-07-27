// Package adapters implements the presence Store over Valkey and the
// Publisher/sources over NATS. Presence is ephemeral — nothing here is
// durable (valkey-keyspace.md invariant).
package adapters

import (
	"context"
	"fmt"
	"strconv"

	"github.com/redis/go-redis/v9"

	"github.com/whatsapp-v2/server/internal/presence"
)

// PresenceStore tracks per-user online devices in a Valkey hash
// presence:{user} (field device_id → last-heartbeat ms). Every op first
// prunes fields older than OnlineWindow, so a crashed pod's devices expire.
type PresenceStore struct {
	client *redis.Client
	// connect/disconnect return the online-device count AFTER the op (and the
	// count BEFORE, for connect) so the service can detect transitions.
	connect    *redis.Script
	heartbeat  *redis.Script
	disconnect *redis.Script
	snapshot   *redis.Script
}

func NewPresenceStore(client *redis.Client) *PresenceStore {
	// prune removes stale device fields; shared prologue.
	const prune = `
	  local now = tonumber(ARGV[1])
	  local window = tonumber(ARGV[2])
	  local all = redis.call('HGETALL', KEYS[1])
	  for i=1,#all,2 do
	    if tonumber(all[i+1]) < now - window then
	      redis.call('HDEL', KEYS[1], all[i])
	    end
	  end`
	keepTTL := `redis.call('PEXPIRE', KEYS[1], window * 2)`
	return &PresenceStore{
		client: client,
		// returns {before, after}
		connect: redis.NewScript(prune + `
		  local before = redis.call('HLEN', KEYS[1])
		  redis.call('HSET', KEYS[1], ARGV[3], now)
		  ` + keepTTL + `
		  return {before, redis.call('HLEN', KEYS[1])}`),
		heartbeat: redis.NewScript(prune + `
		  redis.call('HSET', KEYS[1], ARGV[3], now)
		  ` + keepTTL + `
		  return 1`),
		// returns after-count
		disconnect: redis.NewScript(prune + `
		  redis.call('HDEL', KEYS[1], ARGV[3])
		  ` + keepTTL + `
		  return redis.call('HLEN', KEYS[1])`),
		// returns {count, maxLastSeen}
		snapshot: redis.NewScript(prune + `
		  local all = redis.call('HGETALL', KEYS[1])
		  local maxls = 0
		  for i=1,#all,2 do
		    local v = tonumber(all[i+1])
		    if v > maxls then maxls = v end
		  end
		  return {redis.call('HLEN', KEYS[1]), maxls}`),
	}
}

func key(userID string) string { return "presence:" + userID }

func windowMS() int64 { return presence.OnlineWindow.Milliseconds() }

func (s *PresenceStore) Connect(ctx context.Context, userID, deviceID string, nowMS int64) (bool, error) {
	v, err := s.connect.Run(ctx, s.client, []string{key(userID)}, nowMS, windowMS(), deviceID).Slice()
	if err != nil {
		return false, fmt.Errorf("presence connect: %w", err)
	}
	before, after := toInt(v[0]), toInt(v[1])
	return before == 0 && after > 0, nil
}

func (s *PresenceStore) Heartbeat(ctx context.Context, userID, deviceID string, nowMS int64) error {
	if err := s.heartbeat.Run(ctx, s.client, []string{key(userID)}, nowMS, windowMS(), deviceID).Err(); err != nil {
		return fmt.Errorf("presence heartbeat: %w", err)
	}
	return nil
}

func (s *PresenceStore) Disconnect(ctx context.Context, userID, deviceID string, nowMS int64) (bool, error) {
	after, err := s.disconnect.Run(ctx, s.client, []string{key(userID)}, nowMS, windowMS(), deviceID).Int64()
	if err != nil {
		return false, fmt.Errorf("presence disconnect: %w", err)
	}
	return after == 0, nil
}

func (s *PresenceStore) Snapshot(ctx context.Context, userID string, nowMS int64) (presence.Update, error) {
	v, err := s.snapshot.Run(ctx, s.client, []string{key(userID)}, nowMS, windowMS()).Slice()
	if err != nil {
		return presence.Update{}, fmt.Errorf("presence snapshot: %w", err)
	}
	count, lastSeen := toInt(v[0]), toInt(v[1])
	return presence.Update{UserID: userID, Online: count > 0, LastSeenMS: lastSeen}, nil
}

// toInt coerces a Lua numeric reply (int64) to int64.
func toInt(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case string:
		i, _ := strconv.ParseInt(n, 10, 64)
		return i
	default:
		return 0
	}
}

var _ presence.Store = (*PresenceStore)(nil)
