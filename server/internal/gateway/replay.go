package gateway

import (
	"sort"
	"sync"

	wsv1 "github.com/whatsapp-v2/server/internal/proto/gen/whatsapp/ws/v1"
)

// maxReplayBuffer bounds the live items a connection may hold while its
// replay runs. Overflow falls back to the slow-consumer policy (1013 +
// reconnect-and-replay) — backlog belongs in the inbox, not pod memory.
const maxReplayBuffer = 1024

// replayGate enforces the interleave rule (ws-gateway-lld §3): while a
// connection replays its inbox, live deliveries buffer; buffered items flush
// only after the replay watermark passes them — per-conversation seq order,
// each seq exactly once. Live traffic published while a conversation's
// replay is still behind it would otherwise reach the client out of order.
type replayGate struct {
	mu       sync.Mutex
	open     bool              // replay finished; live flows directly
	seen     map[string]int64  // conversation → highest replayed seq
	buffered []*wsv1.InboxItem // live items held until the flush
}

// newReplayGate returns a gate; open gates (no replay planned) pass live
// items straight through.
func newReplayGate(open bool) *replayGate {
	return &replayGate{open: open, seen: make(map[string]int64)}
}

// holdLive routes one live item. deliver=true → forward now (gate open).
// overflow=true → the buffer is full; the caller applies the slow-consumer
// policy. Otherwise the item is buffered for the post-replay flush.
func (g *replayGate) holdLive(item *wsv1.InboxItem) (deliver, overflow bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.open {
		return true, false
	}
	if len(g.buffered) >= maxReplayBuffer {
		return false, true
	}
	g.buffered = append(g.buffered, item)
	return false, false
}

// advance records a replayed batch — the watermark the flush dedupes
// against (an item can arrive both live and in the replay snapshot).
func (g *replayGate) advance(items []*wsv1.InboxItem) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, it := range items {
		if it.GetSeq() > g.seen[it.GetConversationId()] {
			g.seen[it.GetConversationId()] = it.GetSeq()
		}
	}
}

// finish opens the gate: buffered items the replay did not already cover are
// handed to flush in (conversation, seq) order, exactly once. flush runs
// under the gate lock so no concurrent live item can jump ahead of the
// flushed backlog; it returns false on a full outbound queue (slow
// consumer), which aborts and reports false. After finish, live items flow
// directly and the gate frees its state.
func (g *replayGate) finish(flush func(items []*wsv1.InboxItem) bool) bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	sort.SliceStable(g.buffered, func(i, j int) bool {
		a, b := g.buffered[i], g.buffered[j]
		if a.GetConversationId() != b.GetConversationId() {
			return a.GetConversationId() < b.GetConversationId()
		}
		return a.GetSeq() < b.GetSeq()
	})
	pending := g.buffered[:0]
	for _, it := range g.buffered {
		if it.GetSeq() <= g.seen[it.GetConversationId()] {
			continue // replay already delivered it
		}
		g.seen[it.GetConversationId()] = it.GetSeq() // drop equal-seq duplicates in the buffer itself
		pending = append(pending, it)
	}
	if len(pending) > 0 && !flush(pending) {
		return false
	}
	g.open = true
	g.buffered, g.seen = nil, nil
	return true
}
