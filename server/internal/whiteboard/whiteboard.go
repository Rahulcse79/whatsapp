// Package whiteboard is the collaborative-whiteboard control plane (T12.02): it
// stores and relays the board's append-only CRDT op-log per conversation, gated
// on membership. The client (@wa/client-core `whiteboard`) owns the merge/render;
// the server persists ops under a monotonic seq and serves incremental sync
// (ops since a cursor). Real-time push rides the existing WS/NATS nudge seam;
// polling the cursor endpoint is the fallback. File-collaboration reuses the same
// op-log substrate.
package whiteboard

import (
	"context"
	"encoding/json"
)

// Op is one board operation (a stroke, an erase tombstone, or a clear barrier).
// Data is the opaque op JSON the client CRDT understands; the server only reads
// the envelope (id/author/seq/kind) for storage + ordering.
type Op struct {
	ID             string
	ConversationID string
	Author         string
	Seq            int64
	Kind           string
	Data           json.RawMessage
}

// OpView is one op over the wire (the raw client op JSON, echoed back).
type OpView = json.RawMessage

// SyncResult is GET …/board/ops?since=N.
type SyncResult struct {
	Ops    []OpView `json:"ops"`
	Cursor int64    `json:"cursor"` // the max seq now known — the client's next `since`
}

// Store persists the op-log and gates on conversation membership.
type Store interface {
	IsMember(ctx context.Context, conversationID, userID string) (bool, error)
	// AppendOp stores an op; idempotent on (conversation, op id).
	AppendOp(ctx context.Context, o Op) error
	// ListOps returns ops with seq > since, ascending, up to limit.
	ListOps(ctx context.Context, conversationID string, since int64, limit int) ([]Op, error)
	// MaxSeq is the current cursor for a conversation's board (0 if empty).
	MaxSeq(ctx context.Context, conversationID string) (int64, error)
}
