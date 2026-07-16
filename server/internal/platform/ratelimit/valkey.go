package ratelimit

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// luaGCRA is the production decision path: one atomic script evaluation per
// check (valkey-keyspace.md §2.3). Time comes from Valkey's own clock (TIME)
// so multi-pod checks share one clock and never disagree under pod skew.
//
// KEYS[1] = rl:{scope}:{key} — TAT in microseconds since epoch
// ARGV[1] = emission interval T (µs)
// ARGV[2] = burst tolerance τ (µs)
// Returns {1, 0} on allow, {0, retry_after_µs} on deny.
const luaGCRA = `
local t = redis.call('TIME')
local now = t[1] * 1000000 + t[2]
local emission = tonumber(ARGV[1])
local tolerance = tonumber(ARGV[2])
local tat = tonumber(redis.call('GET', KEYS[1]))
if not tat or tat < now then
  tat = now
end
if (tat - now) > tolerance then
  return {0, tat - tolerance - now}
end
local newtat = tat + emission
local ttl = math.ceil((newtat - now) / 1000000) + 1
redis.call('SET', KEYS[1], newtat, 'EX', ttl)
return {1, 0}
`

// ValkeyLimiter is the production GCRA backend: atomic, shared across pods,
// keys self-expire (window-sized TTL — nothing durable, per the Valkey
// invariant).
type ValkeyLimiter struct {
	client *redis.Client
	script *redis.Script
}

// NewValkeyLimiter wraps client; the script is EVALSHA-cached automatically.
func NewValkeyLimiter(client *redis.Client) *ValkeyLimiter {
	return &ValkeyLimiter{client: client, script: redis.NewScript(luaGCRA)}
}

// Allow records an attempt against key (convention: "rl:{scope}:{subject}")
// and reports the decision.
//
// On Valkey unavailability it returns an error and callers MUST fail closed
// (reject the request as TRANSIENT_UNAVAILABLE): an unenforced limit under
// infrastructure failure is how abuse incidents start (design-patterns §3).
func (l *ValkeyLimiter) Allow(ctx context.Context, key string, p Params) (Result, error) {
	if err := p.Validate(); err != nil {
		return Result{}, err
	}

	v, err := l.script.Run(ctx, l.client,
		[]string{key},
		p.emission().Microseconds(),
		p.tolerance().Microseconds(),
	).Result()
	if err != nil {
		return Result{}, fmt.Errorf("ratelimit: valkey check for %s: %w", key, err)
	}

	arr, ok := v.([]interface{})
	if !ok || len(arr) != 2 {
		return Result{}, fmt.Errorf("ratelimit: unexpected script reply %T for %s", v, key)
	}
	allowed, aok := arr[0].(int64)
	retryUs, rok := arr[1].(int64)
	if !aok || !rok {
		return Result{}, fmt.Errorf("ratelimit: unexpected script reply values %v for %s", arr, key)
	}

	return Result{
		Allowed:    allowed == 1,
		RetryAfter: time.Duration(retryUs) * time.Microsecond,
	}, nil
}
