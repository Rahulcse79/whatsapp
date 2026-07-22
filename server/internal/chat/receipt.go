package chat

import (
	"context"
	"net/http"

	"github.com/whatsapp-v2/server/internal/platform/httpx"
)

// ReceiptKind mirrors wsv1.ReceiptKind (DELIVERED=1, READ=2).
type ReceiptKind int16

const (
	ReceiptDelivered ReceiptKind = 1
	ReceiptRead      ReceiptKind = 2
)

// ReceiptIn is a receipt submitted by a recipient device: a cumulative
// watermark "I have delivered/read this conversation up to up_to_seq".
type ReceiptIn struct {
	ConversationID string
	Kind           ReceiptKind
	UpToSeq        int64
}

// ReceiptOut is a receipt relayed to a target device, tagged with who it is
// from (the recipient who delivered/read).
type ReceiptOut struct {
	ConversationID string
	Kind           ReceiptKind
	UpToSeq        int64
	FromUserID     string
}

// ReceiptStore resolves receipt fan-out targets and read-receipt privacy.
type ReceiptStore interface {
	// ReceiptTargets returns the active device ids that should receive a
	// receipt for the conversation: every member's active devices except the
	// submitting device (relays to the original sender AND read-syncs the
	// submitter's own other devices). Empty if the submitter is not a member.
	ReceiptTargets(ctx context.Context, conversationID, submitterUserID, submitterDeviceID string) ([]string, error)
	// ReadReceiptsEnabled reports whether the user sends read receipts
	// (privacy.read_receipts != "nobody").
	ReadReceiptsEnabled(ctx context.Context, userID string) (bool, error)
}

// ReceiptRelay pushes a receipt toward one device (dev.{id}.receipt). Like
// message delivery, receipts are best-effort conveniences — never load-bearing.
type ReceiptRelay interface {
	RelayReceipt(ctx context.Context, targetDeviceID string, r ReceiptOut) error
}

// SetReceipts wires the receipt pipeline. Kept separate from NewService so
// the accept-only construction paths (and their tests) stay untouched.
func (s *Service) SetReceipts(store ReceiptStore, relay ReceiptRelay) {
	s.receipts = store
	s.relay = relay
}

// SubmitReceipt validates a recipient's receipt, applies the read-receipt
// privacy gate, and relays it to the conversation's other devices. Receipts
// are cumulative and lossy by design, so a relay failure is logged, not
// surfaced (websocket-protocol.md §3).
func (s *Service) SubmitReceipt(ctx context.Context, fromUserID, fromDeviceID string, r ReceiptIn) error {
	if r.ConversationID == "" || r.UpToSeq <= 0 {
		return httpx.Reject(http.StatusBadRequest, "VALIDATION_RECEIPT", "conversation_id and a positive up_to_seq are required")
	}
	if r.Kind != ReceiptDelivered && r.Kind != ReceiptRead {
		return httpx.Reject(http.StatusBadRequest, "VALIDATION_RECEIPT", "receipt kind must be delivered or read")
	}
	if s.receipts == nil || s.relay == nil {
		return nil // receipts not wired (accept-only deployment/tests)
	}

	// Privacy gate: a user who disabled read receipts sends none. Delivered
	// receipts are never gated (WhatsApp semantics).
	if r.Kind == ReceiptRead {
		enabled, err := s.receipts.ReadReceiptsEnabled(ctx, fromUserID)
		if err != nil {
			return httpx.Transient()
		}
		if !enabled {
			return nil // silently dropped — not an error
		}
	}

	targets, err := s.receipts.ReceiptTargets(ctx, r.ConversationID, fromUserID, fromDeviceID)
	if err != nil {
		return httpx.Transient()
	}
	out := ReceiptOut{
		ConversationID: r.ConversationID,
		Kind:           r.Kind,
		UpToSeq:        r.UpToSeq,
		FromUserID:     fromUserID,
	}
	for _, dev := range targets {
		if err := s.relay.RelayReceipt(ctx, dev, out); err != nil {
			s.log.Warn("receipt relay failed (receipts are lossy)", "device_id", dev, "err", err)
		}
	}
	return nil
}
