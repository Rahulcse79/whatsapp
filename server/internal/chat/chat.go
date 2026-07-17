// Package chat owns the message hot path: accept (dedupe, per-conversation
// sequencing), inbox fan-out, and overlay events (edit/delete/react/pin).
// The server relays ciphertext; nothing here inspects content.
// Design: Docs/05-services/core-api-lld.md §2,
// Docs/02-architecture/data-structures-algorithms.md §1–4, §6.
package chat

// MsgKind mirrors the wire MsgKind enum (websocket-protocol.md). Defined here
// as a domain type; the protobuf binding lands with T0.12.
type MsgKind int16

const (
	KindUnspecified   MsgKind = 0
	KindText          MsgKind = 1
	KindMedia         MsgKind = 2
	KindOverlayEdit   MsgKind = 3
	KindOverlayDelete MsgKind = 4
	KindReaction      MsgKind = 5
	KindPin           MsgKind = 6
	KindStoryKey      MsgKind = 7
	KindSenderKeyDist MsgKind = 8
)

// requiresTarget reports whether the kind is an overlay referencing an
// original message (its overlay_target must be set).
func (k MsgKind) requiresTarget() bool {
	switch k {
	case KindOverlayEdit, KindOverlayDelete, KindReaction, KindPin:
		return true
	default:
		return false
	}
}

// AcceptRequest is one message-send to accept. The sender identity is
// established by the caller (the gateway, from the authenticated connection)
// and is never trusted from the payload.
type AcceptRequest struct {
	SenderUserID   string
	SenderDeviceID string
	ConversationID string
	MsgUUID        string // client UUIDv7 — idempotency key
	Kind           MsgKind
	OverlayTarget  string // original msg_uuid for overlay kinds; empty otherwise
	Ciphertext     []byte // sealed envelope; server-opaque
}

// AcceptResult is the "sent" acknowledgement.
type AcceptResult struct {
	Seq            int64
	ServerTimeMS   int64
	RecipientCount int
	// Deduped is true when this was a duplicate of an already-accepted send;
	// the seq is the original's (identical ack).
	Deduped bool
}
