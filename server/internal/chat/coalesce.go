package chat

import "sync"

// ReceiptCoalescer collapses a burst of cumulative receipts into at most one
// entry per (conversation, kind), keeping the highest seq. The gateway feeds
// it every receipt and Drains on a 250 ms ticker, so N receipts in a window
// become one relayed frame per conversation (DS&A §9, "both directions").
// Cumulative semantics make this lossless: only the latest watermark matters.
type ReceiptCoalescer struct {
	mu      sync.Mutex
	pending map[coalesceKey]int64
}

type coalesceKey struct {
	conv string
	kind ReceiptKind
}

// NewReceiptCoalescer returns an empty coalescer.
func NewReceiptCoalescer() *ReceiptCoalescer {
	return &ReceiptCoalescer{pending: make(map[coalesceKey]int64)}
}

// Submit records a receipt, keeping the maximum seq for its (conversation,
// kind). Safe for concurrent use.
func (c *ReceiptCoalescer) Submit(conversationID string, kind ReceiptKind, upToSeq int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	k := coalesceKey{conv: conversationID, kind: kind}
	if upToSeq > c.pending[k] {
		c.pending[k] = upToSeq
	}
}

// Drain returns the coalesced receipts accumulated since the last drain and
// clears the buffer. One entry per (conversation, kind) with the highest seq.
func (c *ReceiptCoalescer) Drain() []ReceiptIn {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.pending) == 0 {
		return nil
	}
	out := make([]ReceiptIn, 0, len(c.pending))
	for k, seq := range c.pending {
		out = append(out, ReceiptIn{ConversationID: k.conv, Kind: k.kind, UpToSeq: seq})
	}
	c.pending = make(map[coalesceKey]int64)
	return out
}
