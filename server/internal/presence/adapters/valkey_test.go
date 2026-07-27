package adapters

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/whatsapp-v2/server/internal/platform/id"
	"github.com/whatsapp-v2/server/internal/presence"
)

func testStore(t *testing.T) *PresenceStore {
	t.Helper()
	addr := os.Getenv("WA_TEST_VALKEY_ADDR")
	if addr == "" {
		t.Skip("WA_TEST_VALKEY_ADDR not set — runs in the CI integration job")
	}
	client := redis.NewClient(&redis.Options{Addr: addr})
	if err := client.Ping(context.Background()).Err(); err != nil {
		t.Fatalf("valkey ping: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return NewPresenceStore(client)
}

func TestIntegration_Presence_MultiDeviceTransitions(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	user := id.New()
	now := time.Now().UnixMilli()

	// First device online → user transitions online.
	online, err := s.Connect(ctx, user, "devA", now)
	if err != nil || !online {
		t.Fatalf("first connect: online=%v err=%v, want true", online, err)
	}
	// Second device → no transition (already online).
	online, err = s.Connect(ctx, user, "devB", now)
	if err != nil || online {
		t.Fatalf("second connect: online=%v, want false (already online)", online)
	}

	snap, err := s.Snapshot(ctx, user, now)
	if err != nil || !snap.Online || snap.LastSeenMS != now {
		t.Fatalf("snapshot: %+v err=%v, want online with last_seen=%d", snap, err, now)
	}

	// One device leaves → still online (the other remains).
	offline, err := s.Disconnect(ctx, user, "devA", now)
	if err != nil || offline {
		t.Fatalf("disconnect devA: offline=%v, want false", offline)
	}
	// Last device leaves → user transitions offline.
	offline, err = s.Disconnect(ctx, user, "devB", now)
	if err != nil || !offline {
		t.Fatalf("disconnect devB: offline=%v, want true", offline)
	}
	snap, _ = s.Snapshot(ctx, user, now)
	if snap.Online {
		t.Fatal("user should be offline after last device left")
	}
}

// A device that stopped heartbeating longer than OnlineWindow is pruned, so a
// crashed pod can't pin a user online forever.
func TestIntegration_Presence_StaleDevicePruned(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	user := id.New()
	past := time.Now().Add(-2 * presence.OnlineWindow).UnixMilli()

	if _, err := s.Connect(ctx, user, "ghost", past); err != nil {
		t.Fatal(err)
	}
	// "now" is well past the window: the ghost device must be pruned.
	now := time.Now().UnixMilli()
	snap, err := s.Snapshot(ctx, user, now)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Online {
		t.Fatal("stale device was not pruned — user still shows online")
	}

	// A fresh connect after the ghost is pruned counts as a new online.
	online, err := s.Connect(ctx, user, "fresh", now)
	if err != nil || !online {
		t.Fatalf("post-prune connect: online=%v, want true", online)
	}
}

func TestIntegration_Presence_HeartbeatKeepsOnline(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	user := id.New()
	t0 := time.Now().Add(-2 * presence.OnlineWindow).UnixMilli()

	if _, err := s.Connect(ctx, user, "d1", t0); err != nil {
		t.Fatal(err)
	}
	// Heartbeat now refreshes liveness; the device must survive a later prune.
	now := time.Now().UnixMilli()
	if err := s.Heartbeat(ctx, user, "d1", now); err != nil {
		t.Fatal(err)
	}
	snap, _ := s.Snapshot(ctx, user, now)
	if !snap.Online {
		t.Fatal("heartbeat did not keep the device online")
	}
}
