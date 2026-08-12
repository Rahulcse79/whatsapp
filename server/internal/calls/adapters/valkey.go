// Package adapters implements call-ctl's ports over Valkey (ring state), Postgres
// (call history + device resolution), and NATS (WS call frames + VoIP push).
package adapters

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/whatsapp-v2/server/internal/calls"
	"github.com/whatsapp-v2/server/internal/calls/domain"
)

// RingStore implements calls.RingStore. Ring state is short-lived and mutated
// per transition, so Valkey (not PG) holds it: a per-ring JSON blob, a room→ring
// index for rejoin/webhooks, and a deadline-scored sorted set the miss sweeper
// scans.
type RingStore struct{ client *redis.Client }

func NewRingStore(client *redis.Client) *RingStore { return &RingStore{client: client} }

func ringKey(ringID string) string { return "call_ring:" + ringID }
func roomKey(roomID string) string { return "call_room:" + roomID }

const ringingZSet = "call_ringing"

func (s *RingStore) Save(ctx context.Context, rec calls.RingRecord, ttl time.Duration) error {
	blob, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	pipe := s.client.TxPipeline()
	pipe.Set(ctx, ringKey(rec.RingID), blob, ttl)
	pipe.Set(ctx, roomKey(rec.RoomID), rec.RingID, ttl)
	if rec.State == domain.StateRinging {
		pipe.ZAdd(ctx, ringingZSet, redis.Z{Score: float64(rec.Deadline.Unix()), Member: rec.RingID})
	} else {
		// Any terminal/answered state leaves the ringing index (nothing to sweep).
		pipe.ZRem(ctx, ringingZSet, rec.RingID)
	}
	_, err = pipe.Exec(ctx)
	return err
}

func (s *RingStore) Get(ctx context.Context, ringID string) (calls.RingRecord, error) {
	blob, err := s.client.Get(ctx, ringKey(ringID)).Bytes()
	if errors.Is(err, redis.Nil) {
		return calls.RingRecord{}, calls.ErrNotFound
	}
	if err != nil {
		return calls.RingRecord{}, err
	}
	var rec calls.RingRecord
	if err := json.Unmarshal(blob, &rec); err != nil {
		return calls.RingRecord{}, err
	}
	return rec, nil
}

func (s *RingStore) GetByRoom(ctx context.Context, roomID string) (calls.RingRecord, error) {
	ringID, err := s.client.Get(ctx, roomKey(roomID)).Result()
	if errors.Is(err, redis.Nil) {
		return calls.RingRecord{}, calls.ErrNotFound
	}
	if err != nil {
		return calls.RingRecord{}, err
	}
	return s.Get(ctx, ringID)
}

func (s *RingStore) ExpiredRinging(ctx context.Context, now time.Time, limit int) ([]calls.RingRecord, error) {
	ids, err := s.client.ZRangeByScore(ctx, ringingZSet, &redis.ZRangeBy{
		Min: "-inf", Max: strconv.FormatInt(now.Unix(), 10), Count: int64(limit),
	}).Result()
	if err != nil {
		return nil, err
	}
	out := make([]calls.RingRecord, 0, len(ids))
	for _, ringID := range ids {
		rec, err := s.Get(ctx, ringID)
		if errors.Is(err, calls.ErrNotFound) {
			s.client.ZRem(ctx, ringingZSet, ringID) // ring blob expired — clean the index
			continue
		}
		if err != nil {
			return nil, err
		}
		if rec.State == domain.StateRinging {
			out = append(out, rec)
		}
	}
	return out, nil
}

var _ calls.RingStore = (*RingStore)(nil)
