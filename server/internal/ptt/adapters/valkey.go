package adapters

import (
	"context"
	"fmt"
	"strconv"

	"github.com/redis/go-redis/v9"

	"github.com/whatsapp-v2/server/internal/ptt/domain"
)

// luaFloor owns the PTT floor atomically (valkey-keyspace §2.2, DS&A §8): one
// script, all three keys in the `{room}` hash slot, no multi-step race windows.
// Time comes from Valkey's own clock (TIME) so pods never disagree. The floor
// value is `device|fence|expiryMs`; lapse is by comparing expiry to now (an
// evicted holder is returned as `demoted` so the caller mutes it). Every grant
// bumps the monotonic fence (the fenced token).
//
// KEYS = floor, queue, fence.  ARGV = action, device, ttlMs, maxQueue.
// Returns {status, fence, position, promotedDevice, promotedFence, demoted}.
const luaFloor = `
local action, device = ARGV[1], ARGV[2]
local ttl, maxq = tonumber(ARGV[3]), tonumber(ARGV[4])
local FLOOR, QUEUE, FENCE = KEYS[1], KEYS[2], KEYS[3]
local t = redis.call('TIME')
local now = tonumber(t[1]) * 1000 + math.floor(tonumber(t[2]) / 1000)

local function setFloor(dev, fence, expiry)
  redis.call('SET', FLOOR, dev .. '|' .. fence .. '|' .. expiry, 'PX', ttl + 5000)
end
local function grantTo(dev)
  local fence = redis.call('INCR', FENCE)
  setFloor(dev, fence, now + ttl)
  redis.call('LREM', QUEUE, 0, dev)
  return fence
end
local function qpos(dev)
  local q = redis.call('LRANGE', QUEUE, 0, -1)
  for i = 1, #q do if q[i] == dev then return i end end
  return -1
end

local holder, hfence
local v = redis.call('GET', FLOOR)
local demoted = ''
if v then
  local p1 = string.find(v, '|'); local p2 = string.find(v, '|', p1 + 1)
  holder = string.sub(v, 1, p1 - 1); hfence = tonumber(string.sub(v, p1 + 1, p2 - 1))
  local hexpiry = tonumber(string.sub(v, p2 + 1))
  if hexpiry <= now then demoted = holder; redis.call('DEL', FLOOR); holder = nil end
end

if action == 'acquire' then
  if holder == nil then
    local head = redis.call('LINDEX', QUEUE, 0)
    if head and head ~= device then
      redis.call('LPOP', QUEUE)
      local pf = grantTo(head)
      local pos = qpos(device)
      if pos == -1 then redis.call('RPUSH', QUEUE, device); pos = redis.call('LLEN', QUEUE) end
      return {'queued', 0, pos, head, pf, demoted}
    end
    if head == device then redis.call('LPOP', QUEUE) end
    return {'granted', grantTo(device), 0, '', 0, demoted}
  end
  if holder == device then setFloor(device, hfence, now + ttl); return {'granted', hfence, 0, '', 0, ''} end
  local pos = qpos(device)
  if pos == -1 then
    if redis.call('LLEN', QUEUE) >= maxq then return {'full', 0, 0, '', 0, demoted} end
    redis.call('RPUSH', QUEUE, device); pos = redis.call('LLEN', QUEUE)
  end
  return {'queued', 0, pos, '', 0, demoted}
elseif action == 'heartbeat' then
  if holder == device then setFloor(device, hfence, now + ttl); return {'ok', hfence, 0, '', 0, ''} end
  return {'lost', 0, 0, '', 0, ''}
elseif action == 'release' then
  if holder == device then
    redis.call('DEL', FLOOR)
    local head = redis.call('LPOP', QUEUE)
    if head then local f = grantTo(head); return {'released', f, 0, head, f, ''} end
    return {'released', 0, 0, '', 0, ''}
  end
  return {'noop', 0, 0, '', 0, ''}
elseif action == 'sweep' then
  if holder == nil and redis.call('LLEN', QUEUE) > 0 then
    local head = redis.call('LPOP', QUEUE); local f = grantTo(head)
    return {'swept', 0, 0, head, f, demoted}
  end
  return {'swept', 0, 0, '', 0, demoted}
end
return {'noop', 0, 0, '', 0, ''}
`

const roomsSet = "ptt:rooms"

// ValkeyFloorStore is the distributed FloorStore.
type ValkeyFloorStore struct {
	client *redis.Client
	script *redis.Script
}

func NewValkeyFloorStore(client *redis.Client) *ValkeyFloorStore {
	return &ValkeyFloorStore{client: client, script: redis.NewScript(luaFloor)}
}

// keys returns the three floor keys, sharing the `{room}` hash tag so they land
// in one cluster slot (single-key-slot atomicity).
func keys(room string) []string {
	tag := "{" + room + "}"
	return []string{"ptt:" + tag + ":floor", "ptt:" + tag + ":queue", "ptt:" + tag + ":fence"}
}

type floorResult struct {
	status        string
	fence         int64
	position      int
	promotedDev   string
	promotedFence int64
	demoted       string
}

func (s *ValkeyFloorStore) run(ctx context.Context, room, action, device string) (floorResult, error) {
	raw, err := s.script.Run(ctx, s.client, keys(room), action, device, int64(domain.FloorTTL.Milliseconds()), domain.MaxQueue).Result()
	if err != nil {
		return floorResult{}, err
	}
	arr, ok := raw.([]any)
	if !ok || len(arr) < 6 {
		return floorResult{}, fmt.Errorf("ptt: unexpected floor script result %v", raw)
	}
	return floorResult{
		status:        asString(arr[0]),
		fence:         asInt(arr[1]),
		position:      int(asInt(arr[2])),
		promotedDev:   asString(arr[3]),
		promotedFence: asInt(arr[4]),
		demoted:       asString(arr[5]),
	}, nil
}

func (s *ValkeyFloorStore) Acquire(ctx context.Context, room, participant string) (domain.AcquireResult, error) {
	r, err := s.run(ctx, room, "acquire", participant)
	if err != nil {
		return domain.AcquireResult{}, err
	}
	s.client.SAdd(ctx, roomsSet, room) // best-effort sweep index
	out := domain.AcquireResult{Position: r.position, Full: r.status == "full", Demoted: r.demoted, Promoted: grant(r.promotedDev, r.promotedFence)}
	if r.status == "granted" {
		out.Granted = &domain.Grant{Device: participant, Fence: r.fence}
	}
	return out, nil
}

func (s *ValkeyFloorStore) Heartbeat(ctx context.Context, room, participant string) (domain.HeartbeatResult, error) {
	r, err := s.run(ctx, room, "heartbeat", participant)
	if err != nil {
		return domain.HeartbeatResult{}, err
	}
	return domain.HeartbeatResult{Held: r.status == "ok", Fence: r.fence}, nil
}

func (s *ValkeyFloorStore) Release(ctx context.Context, room, participant string) (domain.ReleaseResult, error) {
	r, err := s.run(ctx, room, "release", participant)
	if err != nil {
		return domain.ReleaseResult{}, err
	}
	return domain.ReleaseResult{Released: r.status == "released", Next: grant(r.promotedDev, r.promotedFence)}, nil
}

func (s *ValkeyFloorStore) Sweep(ctx context.Context, room string) (domain.SweepResult, error) {
	r, err := s.run(ctx, room, "sweep", "")
	if err != nil {
		return domain.SweepResult{}, err
	}
	// Reclaim the sweep index once a room is fully idle.
	if r.promotedDev == "" {
		if n, _ := s.client.Exists(ctx, keys(room)[0]).Result(); n == 0 {
			if l, _ := s.client.LLen(ctx, keys(room)[1]).Result(); l == 0 {
				s.client.SRem(ctx, roomsSet, room)
			}
		}
	}
	return domain.SweepResult{Demoted: r.demoted, Promoted: grant(r.promotedDev, r.promotedFence)}, nil
}

func (s *ValkeyFloorStore) ActiveRooms(ctx context.Context) ([]string, error) {
	return s.client.SMembers(ctx, roomsSet).Result()
}

func (s *ValkeyFloorStore) Snapshot(ctx context.Context, room string) (string, int, error) {
	k := keys(room)
	v, err := s.client.Get(ctx, k[0]).Result()
	holder := ""
	if err == nil && v != "" {
		if p1 := indexByte(v, '|'); p1 >= 0 {
			holder = v[:p1] // best-effort; the Lua path owns lapse eviction
		}
	} else if err != nil && err != redis.Nil {
		return "", 0, err
	}
	n, err := s.client.LLen(ctx, k[1]).Result()
	if err != nil {
		return "", 0, err
	}
	return holder, int(n), nil
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

func grant(device string, fence int64) *domain.Grant {
	if device == "" {
		return nil
	}
	return &domain.Grant{Device: device, Fence: fence}
}

func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func asInt(v any) int64 {
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
