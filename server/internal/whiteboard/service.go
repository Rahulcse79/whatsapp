package whiteboard

import (
	"context"
	"net/http"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
	"github.com/whatsapp-v2/server/internal/whiteboard/domain"
)

const syncLimit = 2000

// Service runs the whiteboard op-log: append + incremental sync, membership-gated.
type Service struct {
	store Store
}

func NewService(store Store) *Service { return &Service{store: store} }

// Append stores a batch of board ops (a stroke/erase/clear). The author is forced
// to the caller — a client can't attribute an op to someone else. Idempotent on
// op id, so a retried append is safe.
func (s *Service) Append(ctx context.Context, ident auth.Identity, conversationID string, ops []Op) error {
	if err := s.requireMember(ctx, conversationID, ident.UserID); err != nil {
		return err
	}
	if len(ops) == 0 || len(ops) > domain.MaxBatch {
		return httpx.Reject(http.StatusBadRequest, "VALIDATION_BATCH", "send between 1 and 200 ops")
	}
	for i := range ops {
		o := &ops[i]
		if err := domain.ValidateOp(o.ID, o.Kind, o.Seq, len(o.Data)); err != nil {
			return httpx.Reject(http.StatusBadRequest, "VALIDATION_OP", err.Error())
		}
		o.ConversationID = conversationID
		o.Author = ident.UserID
		if err := s.store.AppendOp(ctx, *o); err != nil {
			return httpx.Transient()
		}
	}
	return nil
}

// Sync returns ops with seq > since plus the new cursor — the incremental poll
// that keeps every client's CRDT convergent.
func (s *Service) Sync(ctx context.Context, ident auth.Identity, conversationID string, since int64) (SyncResult, error) {
	if err := s.requireMember(ctx, conversationID, ident.UserID); err != nil {
		return SyncResult{}, err
	}
	ops, err := s.store.ListOps(ctx, conversationID, since, syncLimit)
	if err != nil {
		return SyncResult{}, httpx.Transient()
	}
	cursor := since
	views := make([]OpView, len(ops))
	for i, o := range ops {
		views[i] = o.Data
		if o.Seq > cursor {
			cursor = o.Seq
		}
	}
	return SyncResult{Ops: views, Cursor: cursor}, nil
}

func (s *Service) requireMember(ctx context.Context, conversationID, userID string) error {
	ok, err := s.store.IsMember(ctx, conversationID, userID)
	if err != nil {
		return httpx.Transient()
	}
	if !ok {
		return httpx.Reject(http.StatusNotFound, "BOARD_NOT_FOUND", "not found")
	}
	return nil
}
