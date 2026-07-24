package gateway

import (
	"context"
	"sync"
	"time"

	wsv1 "github.com/whatsapp-v2/server/internal/proto/gen/whatsapp/ws/v1"
)

// receiptFlushInterval is the coalescing window: at most one SubmitReceipt
// per (conversation, kind) per window, whatever the client sends (DS&A §9).
const receiptFlushInterval = 250 * time.Millisecond

// receiptCoalescer collapses inbound Receipt frames to the highest cumulative
// seq per (conversation, kind) between flushes. wsv1-typed twin of
// chat.ReceiptCoalescer — the gateway speaks wire types, not chat domain
// types (context boundary).
type receiptCoalescer struct {
	mu      sync.Mutex
	pending map[receiptKey]int64
}

type receiptKey struct {
	conv string
	kind wsv1.ReceiptKind
}

func newReceiptCoalescer() *receiptCoalescer {
	return &receiptCoalescer{pending: make(map[receiptKey]int64)}
}

// submit records a receipt, keeping the max seq. Junk (empty conversation,
// non-positive seq, unknown kind) is dropped here — core-api never sees it.
func (c *receiptCoalescer) submit(conv string, kind wsv1.ReceiptKind, seq int64) {
	if conv == "" || seq <= 0 ||
		(kind != wsv1.ReceiptKind_RECEIPT_KIND_DELIVERED && kind != wsv1.ReceiptKind_RECEIPT_KIND_READ) {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	k := receiptKey{conv: conv, kind: kind}
	if seq > c.pending[k] {
		c.pending[k] = seq
	}
}

// drain returns and clears the coalesced receipts.
func (c *receiptCoalescer) drain() []*wsv1.Receipt {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.pending) == 0 {
		return nil
	}
	out := make([]*wsv1.Receipt, 0, len(c.pending))
	for k, seq := range c.pending {
		out = append(out, &wsv1.Receipt{ConversationId: k.conv, Kind: k.kind, UpToSeq: seq})
	}
	c.pending = make(map[receiptKey]int64)
	return out
}

// receiptFlushLoop forwards the connection's coalesced receipts to core-api
// every window, with a best-effort final flush on disconnect. Receipts are
// lossy conveniences: failures are logged, never fatal to the connection.
func (s *Server) receiptFlushLoop(ctx context.Context, c *Conn) {
	t := time.NewTicker(receiptFlushInterval)
	defer t.Stop()

	flush := func() {
		for _, r := range c.rcoal.drain() {
			// Background-based timeout so the final flush after ctx
			// cancellation still goes out.
			rctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
			err := s.chat.SubmitReceipt(rctx, c.userID, c.deviceID, r)
			cancel()
			if err != nil {
				s.log.Warn("receipt submit failed (receipts are lossy)",
					"device_id", c.deviceID, "err", err)
			}
		}
	}

	for {
		select {
		case <-ctx.Done():
			flush()
			return
		case <-t.C:
			flush()
		}
	}
}
