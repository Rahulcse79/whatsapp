package chat

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/whatsapp-v2/server/internal/chat/domain"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
	"github.com/whatsapp-v2/server/internal/platform/id"
)

// InboxTTL is how long undelivered ciphertext survives server-side (ADR-001).
const InboxTTL = 30 * 24 * time.Hour

// ErrNotMember is returned by the store when the sender is not in the
// conversation.
var ErrNotMember = errors.New("chat: sender is not a conversation member")

// AcceptParams is the store's transactional input (IDs pre-validated).
type AcceptParams struct {
	SenderUserID   string
	SenderDeviceID string
	ConversationID string
	MsgUUID        string
	Kind           MsgKind
	OverlayTarget  string // "" = NULL
	Ciphertext     []byte
	AcceptedAt     time.Time
	ExpiresAt      time.Time
}

// StoreResult is the outcome of the accept transaction.
type StoreResult struct {
	Seq                int64
	RecipientDeviceIDs []string
}

// Store is the persistence port: the seq+inbox transaction.
type Store interface {
	// Accept assigns the next conversation seq and writes one inbox row per
	// recipient device (all conversation members' active devices except the
	// sending device), in a single transaction. Returns ErrNotMember if the
	// sender is not in the conversation.
	Accept(ctx context.Context, p AcceptParams) (StoreResult, error)
}

// Publisher is the live-delivery port: fan the accepted item out to each
// recipient device's subject (dev.{id}.out). NATS is TRANSIT, not truth —
// the inbox row is the durability guarantee, so publish failures are logged,
// never surfaced to the sender (internal-events-nats.md).
type Publisher interface {
	PublishDelivery(ctx context.Context, recipientDeviceID string, item InboxItem) error
}

// ReplayBatchSize is the server-side cap on items per replay batch
// (rpc.proto PullInboxRequest.batch_size).
const ReplayBatchSize = 100

// Inbox is the replay/ack port over the message inbox (T0.13).
type Inbox interface {
	// PullInbox streams the device's pending items in (conversation, seq)
	// order, batched at most batchSize at a time, skipping anything at or
	// below the given cursors. emit returning an error aborts the pull.
	PullInbox(ctx context.Context, deviceID string, cursors []Cursor, batchSize int, emit func(batch []InboxItem) error) error
	// AckDelivered deletes every row covered by the cumulative cursors.
	// Idempotent: replaying a cursor is a no-op.
	AckDelivered(ctx context.Context, deviceID string, upTo []Cursor) error
}

// Deduper is the idempotency port over Valkey.
type Deduper interface {
	// Claim attempts to reserve msgUUID. won=true means this caller owns the
	// accept. Otherwise, if priorSeq>0 the send already completed (return that
	// seq for an identical ack); if pending, another accept is in-flight.
	Claim(ctx context.Context, msgUUID string) (won bool, priorSeq int64, pending bool, err error)
	// Commit records the assigned seq so future duplicates get an identical ack.
	Commit(ctx context.Context, msgUUID string, seq int64) error
	// Release drops a claim after a failed accept so a retry can re-claim.
	Release(ctx context.Context, msgUUID string) error
}

// Service is the accept pipeline plus the replay/ack and receipt surfaces.
type Service struct {
	store    Store
	inbox    Inbox // nil = replay/ack unavailable (accept-only tests)
	dedupe   Deduper
	pub      Publisher    // nil = no live delivery (tests); inbox still holds everything
	receipts ReceiptStore // nil = receipts not wired (set via SetReceipts)
	relay    ReceiptRelay // nil = receipts not wired
	log      *slog.Logger
	now      func() time.Time
}

func NewService(store Store, inbox Inbox, dedupe Deduper, pub Publisher, log *slog.Logger) *Service {
	return &Service{store: store, inbox: inbox, dedupe: dedupe, pub: pub, log: log, now: time.Now}
}

// Accept is the hot path (target < 10 ms server time): validate → dedupe →
// sequence+inbox → ack. Every step is idempotent so client retries are safe.
func (s *Service) Accept(ctx context.Context, req AcceptRequest) (AcceptResult, error) {
	// 1. Structural validation — cheap, before touching any store.
	if _, err := id.Parse(req.MsgUUID); err != nil {
		return AcceptResult{}, httpx.Reject(http.StatusBadRequest, "VALIDATION_MSG_ID", "msg_uuid must be a UUIDv7")
	}
	if len(req.Ciphertext) == 0 {
		return AcceptResult{}, httpx.Reject(http.StatusBadRequest, "VALIDATION_EMPTY", "empty ciphertext")
	}
	if req.Kind.requiresTarget() {
		if req.OverlayTarget == "" {
			return AcceptResult{}, httpx.Reject(http.StatusBadRequest, "VALIDATION_OVERLAY", "overlay requires overlay_target")
		}
		if _, err := id.Parse(req.OverlayTarget); err != nil {
			return AcceptResult{}, httpx.Reject(http.StatusBadRequest, "VALIDATION_OVERLAY", "overlay_target must be a UUIDv7")
		}
	}

	now := s.now()

	// 2. Overlay window (edit ≤ 15 min, delete ≤ 48 h), anchored to the
	// target's UUIDv7 timestamp (domain.CheckOverlayWindow).
	isEdit := req.Kind == KindOverlayEdit
	isDelete := req.Kind == KindOverlayDelete
	switch domain.CheckOverlayWindow(isEdit, isDelete, req.OverlayTarget, now) {
	case domain.WindowOK:
	case domain.WindowEditClosed:
		return AcceptResult{}, httpx.Reject(http.StatusForbidden, "VALIDATION_EDIT_WINDOW_CLOSED", "edit window (15m) has closed")
	case domain.WindowDeleteClosed:
		return AcceptResult{}, httpx.Reject(http.StatusForbidden, "VALIDATION_DELETE_WINDOW_CLOSED", "delete-for-everyone window (48h) has closed")
	case domain.WindowBadTarget:
		return AcceptResult{}, httpx.Reject(http.StatusBadRequest, "VALIDATION_OVERLAY", "overlay_target must be a UUIDv7")
	}

	// 3. Dedupe claim. Fail CLOSED on Valkey error: a duplicate delivered to
	// the user is worse than a retry (design-patterns §3).
	won, priorSeq, pending, err := s.dedupe.Claim(ctx, req.MsgUUID)
	if err != nil {
		s.log.Error("dedupe claim unavailable — failing closed", "err", err)
		return AcceptResult{}, httpx.Transient()
	}
	if !won {
		if pending {
			// A concurrent accept of the same uuid is in flight; the client
			// retries and will then get the committed seq.
			return AcceptResult{}, httpx.Transient()
		}
		return AcceptResult{Seq: priorSeq, ServerTimeMS: now.UnixMilli(), Deduped: true}, nil
	}

	// 4. Assign seq + fan out inbox rows in one transaction.
	res, err := s.store.Accept(ctx, AcceptParams{
		SenderUserID:   req.SenderUserID,
		SenderDeviceID: req.SenderDeviceID,
		ConversationID: req.ConversationID,
		MsgUUID:        req.MsgUUID,
		Kind:           req.Kind,
		OverlayTarget:  req.OverlayTarget,
		Ciphertext:     req.Ciphertext,
		AcceptedAt:     now,
		ExpiresAt:      now.Add(InboxTTL),
	})
	if err != nil {
		// Release the claim so the client's retry can re-accept.
		if rerr := s.dedupe.Release(ctx, req.MsgUUID); rerr != nil {
			s.log.Error("dedupe release failed", "msg_uuid", req.MsgUUID, "err", rerr)
		}
		if errors.Is(err, ErrNotMember) {
			return AcceptResult{}, httpx.Reject(http.StatusForbidden, "STATE_NOT_MEMBER", "not a member of this conversation")
		}
		s.log.Error("accept transaction failed", "err", err)
		return AcceptResult{}, httpx.Transient()
	}

	// 5. Record the seq for identical-ack on future duplicates. A failure
	// here does not fail the accept (the message is durably stored); it only
	// risks a duplicate on an unlikely exact-retry, which the inbox PK still
	// dedupes at the row level.
	if err := s.dedupe.Commit(ctx, req.MsgUUID, res.Seq); err != nil {
		s.log.Error("dedupe commit failed", "msg_uuid", req.MsgUUID, "err", err)
	}

	// 6. Live delivery (best-effort): connected devices get the item now;
	// everyone else gets it via inbox replay. A publish failure is a latency
	// event, never a loss event.
	if s.pub != nil {
		item := InboxItem{
			ConversationID: req.ConversationID,
			Seq:            res.Seq,
			MsgUUID:        req.MsgUUID,
			SenderUserID:   req.SenderUserID,
			SenderDeviceID: req.SenderDeviceID,
			Kind:           req.Kind,
			OverlayTarget:  req.OverlayTarget,
			Ciphertext:     req.Ciphertext,
			AcceptedAtMS:   now.UnixMilli(),
		}
		for _, did := range res.RecipientDeviceIDs {
			if err := s.pub.PublishDelivery(ctx, did, item); err != nil {
				s.log.Warn("live delivery publish failed (inbox will cover it)",
					"device_id", did, "err", err)
			}
		}
	}

	return AcceptResult{
		Seq:            res.Seq,
		ServerTimeMS:   now.UnixMilli(),
		RecipientCount: len(res.RecipientDeviceIDs),
	}, nil
}

// PullInbox streams the device's pending inbox in (conversation, seq) order.
// cursors narrow the replay to items the client has not yet persisted; empty
// cursors replay everything pending (fresh session). batchSize is clamped to
// ReplayBatchSize — the caller never dictates unbounded batches.
func (s *Service) PullInbox(ctx context.Context, deviceID string, cursors []Cursor, batchSize int, emit func(batch []InboxItem) error) error {
	if deviceID == "" {
		return httpx.Reject(http.StatusBadRequest, "VALIDATION_DEVICE", "recipient_device_id is required")
	}
	if batchSize <= 0 || batchSize > ReplayBatchSize {
		batchSize = ReplayBatchSize
	}
	return s.inbox.PullInbox(ctx, deviceID, cursors, batchSize, emit)
}

// AckDelivered applies the client's cumulative persistence watermark:
// everything at or below each cursor is deleted from the inbox
// (ACK-after-persist — websocket-protocol.md §3). Idempotent.
func (s *Service) AckDelivered(ctx context.Context, deviceID string, upTo []Cursor) error {
	if deviceID == "" {
		return httpx.Reject(http.StatusBadRequest, "VALIDATION_DEVICE", "recipient_device_id is required")
	}
	// Drop structurally empty cursors rather than failing the batch: acks are
	// fire-and-forget from the client's perspective and must stay idempotent.
	valid := upTo[:0]
	for _, c := range upTo {
		if c.ConversationID != "" && c.LastSeq > 0 {
			valid = append(valid, c)
		}
	}
	if len(valid) == 0 {
		return nil
	}
	return s.inbox.AckDelivered(ctx, deviceID, valid)
}
