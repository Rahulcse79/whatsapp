package adapters

import (
	"context"
	"os"
	"testing"

	"github.com/redis/go-redis/v9"

	"github.com/whatsapp-v2/server/internal/platform/id"
)

func testFloorStore(t *testing.T) (*ValkeyFloorStore, string) {
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
	// A unique room per test run so keys don't collide across reruns.
	return NewValkeyFloorStore(client), "room-" + id.New()
}

func TestIntegration_Floor_AcquireQueueRelease(t *testing.T) {
	s, room := testFloorStore(t)
	ctx := context.Background()

	// First speaker gets the floor with fence 1.
	a, err := s.Acquire(ctx, room, "u1:d1")
	if err != nil {
		t.Fatal(err)
	}
	if a.Granted == nil || a.Granted.Fence != 1 {
		t.Fatalf("acquire = %+v, want granted fence 1", a)
	}

	// Second speaker queues at position 1.
	b, err := s.Acquire(ctx, room, "u2:d2")
	if err != nil {
		t.Fatal(err)
	}
	if b.Granted != nil || b.Position != 1 {
		t.Fatalf("acquire b = %+v, want queued pos 1", b)
	}

	// Re-acquire by the holder refreshes (same fence).
	if a2, _ := s.Acquire(ctx, room, "u1:d1"); a2.Granted == nil || a2.Granted.Fence != 1 {
		t.Fatalf("re-acquire = %+v, want granted fence 1", a2)
	}

	// Release promotes the queue head with the next fence.
	r, err := s.Release(ctx, room, "u1:d1")
	if err != nil {
		t.Fatal(err)
	}
	if !r.Released || r.Next == nil || r.Next.Device != "u2:d2" || r.Next.Fence != 2 {
		t.Fatalf("release = %+v, want promote u2 fence 2", r)
	}

	// The active-rooms index knows this room.
	rooms, _ := s.ActiveRooms(ctx)
	found := false
	for _, rm := range rooms {
		if rm == room {
			found = true
		}
	}
	if !found {
		t.Fatalf("active rooms %v should include %s", rooms, room)
	}

	// Final release empties the room; the fence stays monotonic across a fresh
	// acquire.
	if _, err := s.Release(ctx, room, "u2:d2"); err != nil {
		t.Fatal(err)
	}
	if a3, _ := s.Acquire(ctx, room, "u3:d3"); a3.Granted == nil || a3.Granted.Fence != 3 {
		t.Fatalf("post-empty acquire = %+v, want fence 3 (monotonic)", a3)
	}
}

func TestIntegration_Floor_Heartbeat(t *testing.T) {
	s, room := testFloorStore(t)
	ctx := context.Background()
	_, _ = s.Acquire(ctx, room, "u1:d1")

	if hb, _ := s.Heartbeat(ctx, room, "u1:d1"); !hb.Held || hb.Fence != 1 {
		t.Fatalf("holder heartbeat = %+v, want held fence 1", hb)
	}
	if hb, _ := s.Heartbeat(ctx, room, "u2:d2"); hb.Held {
		t.Fatal("non-holder heartbeat must not be held")
	}
	_, _ = s.Release(ctx, room, "u1:d1")
}
