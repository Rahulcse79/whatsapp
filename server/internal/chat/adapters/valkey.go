package adapters

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/whatsapp-v2/server/internal/chat"
)

// dedupeTTL bounds how long an idempotency record lives — long enough to
// absorb any realistic client retry window (valkey-keyspace.md: 24 h).
const dedupeTTL = 24 * time.Hour

// pendingMarker is stored while an accept is in flight, before its seq is
// known. A concurrent duplicate that sees it is told to retry.
const pendingMarker = "P"

// Deduper implements chat.Deduper over Valkey.
type Deduper struct {
	client *redis.Client
	claim  *redis.Script
}

func NewDeduper(client *redis.Client) *Deduper {
	return &Deduper{
		client: client,
		// Atomic get-or-claim: if the key is absent, set the pending marker
		// and report a win; otherwise return the existing value.
		claim: redis.NewScript(`
			local v = redis.call('GET', KEYS[1])
			if v == false then
			  redis.call('SET', KEYS[1], ARGV[1], 'EX', ARGV[2])
			  return {1, ''}
			end
			return {0, v}`),
	}
}

func dedupeKey(msgUUID string) string { return "dedupe:" + msgUUID }

func (d *Deduper) Claim(ctx context.Context, msgUUID string) (won bool, priorSeq int64, pending bool, err error) {
	v, err := d.claim.Run(ctx, d.client, []string{dedupeKey(msgUUID)},
		pendingMarker, int(dedupeTTL.Seconds())).Result()
	if err != nil {
		return false, 0, false, fmt.Errorf("dedupe claim: %w", err)
	}
	arr, ok := v.([]interface{})
	if !ok || len(arr) != 2 {
		return false, 0, false, fmt.Errorf("dedupe claim: unexpected reply %T", v)
	}
	if wonFlag, _ := arr[0].(int64); wonFlag == 1 {
		return true, 0, false, nil
	}
	existing, _ := arr[1].(string)
	if existing == pendingMarker {
		return false, 0, true, nil
	}
	seq, perr := strconv.ParseInt(existing, 10, 64)
	if perr != nil {
		// Corrupt value — treat as pending so the client retries rather than
		// receiving a wrong seq.
		return false, 0, true, nil
	}
	return false, seq, false, nil
}

func (d *Deduper) Commit(ctx context.Context, msgUUID string, seq int64) error {
	if err := d.client.Set(ctx, dedupeKey(msgUUID), seq, dedupeTTL).Err(); err != nil {
		return fmt.Errorf("dedupe commit: %w", err)
	}
	return nil
}

func (d *Deduper) Release(ctx context.Context, msgUUID string) error {
	if err := d.client.Del(ctx, dedupeKey(msgUUID)).Err(); err != nil {
		return fmt.Errorf("dedupe release: %w", err)
	}
	return nil
}

// compile-time assertion that Deduper satisfies the port.
var _ chat.Deduper = (*Deduper)(nil)
