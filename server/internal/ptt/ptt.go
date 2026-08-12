// Package ptt implements server-authoritative push-to-talk floor control: atomic
// Valkey-Lua acquire/release/heartbeat with fencing, FIFO queueing, and SFU
// publish-permission flips (valkey-keyspace §2, DS&A §8, rtc-lld §4). Budget:
// p95 grant ≤ 200 ms.
package ptt

import (
	"context"

	"github.com/whatsapp-v2/server/internal/ptt/domain"
)

// FloorStore is the atomic floor backend (Valkey Lua in prod; in-memory in
// tests). Each op is one round trip; the returned result carries every effect
// the service must apply (grant/queue/promote/demote).
type FloorStore interface {
	Acquire(ctx context.Context, room, participant string) (domain.AcquireResult, error)
	Heartbeat(ctx context.Context, room, participant string) (domain.HeartbeatResult, error)
	Release(ctx context.Context, room, participant string) (domain.ReleaseResult, error)
	Sweep(ctx context.Context, room string) (domain.SweepResult, error)
	// ActiveRooms lists rooms with a held floor or waiters, for the sweep loop.
	ActiveRooms(ctx context.Context) ([]string, error)
	// Snapshot returns the current holder (empty if none) + queue length, for the
	// join response.
	Snapshot(ctx context.Context, room string) (holder string, queueLen int, err error)
}

// RoomState is the join response's floor status.
type RoomState struct {
	CurrentSpeaker string `json:"current_speaker,omitempty"`
	QueueLen       int    `json:"queue_len"`
}

// SFU flips a participant's publish permission for a fence (rtc-lld §4:
// LiveKit UpdateParticipant). Enforcement is media-plane: stale-fence audio is
// dropped at the SFU, so a demoted speaker goes silent.
type SFU interface {
	AllowPublish(ctx context.Context, room, participant string, fence int64) error
	DenyPublish(ctx context.Context, room, participant string) error
}

// Signaler pushes PTT WS frames back to a participant's device.
type Signaler interface {
	Grant(ctx context.Context, room, participant string, fence int64, maxSpeakMS int64) error
	QueuePos(ctx context.Context, room, participant string, position int) error
	Revoke(ctx context.Context, room, participant, reason string) error
}

// participant is "userID:deviceID" — the LiveKit identity + floor key.
func participantOf(userID, deviceID string) string { return userID + ":" + deviceID }

// maxSpeakMS is exported to the wire from the domain cap.
const maxSpeakMS = domain.MaxSpeakMS
