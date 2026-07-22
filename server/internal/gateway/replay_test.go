package gateway

import (
	"testing"

	wsv1 "github.com/whatsapp-v2/server/internal/proto/gen/whatsapp/ws/v1"
)

func item(conv string, seq int64) *wsv1.InboxItem {
	return &wsv1.InboxItem{ConversationId: conv, Seq: seq}
}

// An open gate (no replay planned) forwards live items immediately.
func TestReplayGate_OpenPassesThrough(t *testing.T) {
	g := newReplayGate(true)
	deliver, overflow := g.holdLive(item("c1", 1))
	if !deliver || overflow {
		t.Fatalf("open gate: deliver=%v overflow=%v, want true/false", deliver, overflow)
	}
}

// A closed gate buffers live items and, on finish, flushes exactly the items
// the replay did not already cover, in (conversation, seq) order.
func TestReplayGate_BuffersAndDedupesOnFinish(t *testing.T) {
	g := newReplayGate(false)

	// Replay covered c1 up to seq 5.
	g.advance([]*wsv1.InboxItem{item("c1", 3), item("c1", 5)})

	// Live items arrive (out of order, with overlap) while replaying.
	for _, it := range []*wsv1.InboxItem{
		item("c1", 5), // == watermark → duplicate, must be dropped
		item("c1", 4), // < watermark → duplicate, must be dropped
		item("c1", 7),
		item("c1", 6),
		item("c2", 1), // different conversation, never replayed → kept
	} {
		if deliver, overflow := g.holdLive(it); deliver || overflow {
			t.Fatalf("closed gate should buffer: deliver=%v overflow=%v", deliver, overflow)
		}
	}

	var flushed []*wsv1.InboxItem
	ok := g.finish(func(items []*wsv1.InboxItem) bool {
		flushed = append(flushed, items...)
		return true
	})
	if !ok {
		t.Fatal("finish reported slow consumer unexpectedly")
	}

	// Expect c1:6, c1:7, c2:1 — deduped and ordered; no seq ≤ replayed watermark.
	want := []struct {
		conv string
		seq  int64
	}{{"c1", 6}, {"c1", 7}, {"c2", 1}}
	if len(flushed) != len(want) {
		t.Fatalf("flushed %d items, want %d: %+v", len(flushed), len(want), flushed)
	}
	for i, w := range want {
		if flushed[i].GetConversationId() != w.conv || flushed[i].GetSeq() != w.seq {
			t.Fatalf("flushed[%d] = %s/%d, want %s/%d", i,
				flushed[i].GetConversationId(), flushed[i].GetSeq(), w.conv, w.seq)
		}
	}

	// After finish the gate is open: live items flow straight through.
	if deliver, _ := g.holdLive(item("c1", 8)); !deliver {
		t.Fatal("gate should be open after finish")
	}
}

// Duplicate live items within the buffer (same seq twice) collapse to one.
func TestReplayGate_DropsIntraBufferDuplicates(t *testing.T) {
	g := newReplayGate(false)
	for _, it := range []*wsv1.InboxItem{item("c1", 2), item("c1", 2), item("c1", 2)} {
		g.holdLive(it)
	}
	var count int
	g.finish(func(items []*wsv1.InboxItem) bool { count += len(items); return true })
	if count != 1 {
		t.Fatalf("flushed %d items, want 1 (duplicates collapsed)", count)
	}
}

// A full buffer reports overflow so the caller can apply the slow-consumer
// policy — the backlog belongs in the inbox, not pod memory.
func TestReplayGate_Overflow(t *testing.T) {
	g := newReplayGate(false)
	for i := 0; i < maxReplayBuffer; i++ {
		if _, overflow := g.holdLive(item("c1", int64(i+1))); overflow {
			t.Fatalf("overflow at %d, before the cap", i)
		}
	}
	if _, overflow := g.holdLive(item("c1", maxReplayBuffer+1)); !overflow {
		t.Fatal("expected overflow past the buffer cap")
	}
}

// If the outbound queue is full during the flush, finish reports false so the
// connection is dropped (and will replay on reconnect).
func TestReplayGate_FinishReportsSlowConsumer(t *testing.T) {
	g := newReplayGate(false)
	g.holdLive(item("c1", 1))
	if ok := g.finish(func([]*wsv1.InboxItem) bool { return false }); ok {
		t.Fatal("finish should return false when flush fails")
	}
}
