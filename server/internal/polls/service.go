package polls

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
	"github.com/whatsapp-v2/server/internal/platform/id"
	"github.com/whatsapp-v2/server/internal/polls/domain"
)

// Service orchestrates poll create/vote/close/results. Knowledge of a poll_id
// is the implicit access gate: it's only learned by decrypting the E2EE poll
// message, delivered solely to conversation members (a gRPC membership check is
// a later refinement, as elsewhere).
type Service struct {
	store Store
	now   func() time.Time
	newID func() string
}

func NewService(store Store) *Service {
	return &Service{store: store, now: time.Now, newID: id.New}
}

// Create registers a poll's metadata and returns its id. The client then seals
// a poll message carrying poll_id + the (E2EE) question and options.
func (s *Service) Create(ctx context.Context, ident auth.Identity, conversationID string, optionCount int, multi bool, closesAtMS int64) (CreateResult, error) {
	if conversationID == "" {
		return CreateResult{}, httpx.Reject(http.StatusBadRequest, "VALIDATION_CONVERSATION", "conversation_id required")
	}
	if err := domain.ValidateCreate(optionCount); err != nil {
		return CreateResult{}, httpx.Reject(http.StatusBadRequest, "VALIDATION_OPTIONS", err.Error())
	}
	var closesAt *time.Time
	if closesAtMS > 0 {
		t := time.UnixMilli(closesAtMS)
		closesAt = &t
	}
	p := Poll{
		ID: s.newID(), ConversationID: conversationID, CreatorID: ident.UserID,
		OptionCount: optionCount, Multi: multi, ClosesAt: closesAt, CreatedAt: s.now(),
	}
	if err := s.store.Create(ctx, p); err != nil {
		return CreateResult{}, httpx.Transient()
	}
	return CreateResult{PollID: p.ID}, nil
}

// Vote records the caller's chosen option indices, replacing any prior vote.
// Rejects a closed or expired poll.
func (s *Service) Vote(ctx context.Context, ident auth.Identity, pollID string, indices []int) error {
	p, err := s.load(ctx, pollID)
	if err != nil {
		return err
	}
	if s.isClosed(p) {
		return httpx.Reject(http.StatusConflict, "POLL_CLOSED", "poll is closed")
	}
	if err := domain.ValidateVote(indices, p.OptionCount, p.Multi); err != nil {
		return httpx.Reject(http.StatusBadRequest, "VALIDATION_VOTE", err.Error())
	}
	if err := s.store.ReplaceVotes(ctx, pollID, ident.UserID, indices); err != nil {
		return httpx.Transient()
	}
	return nil
}

// Close ends voting. Creator-only; a non-creator gets 404 (can't probe polls
// they don't own).
func (s *Service) Close(ctx context.Context, ident auth.Identity, pollID string) error {
	p, err := s.load(ctx, pollID)
	if err != nil {
		return err
	}
	if p.CreatorID != ident.UserID {
		return httpx.Reject(http.StatusNotFound, "POLL_NOT_FOUND", "poll not found")
	}
	if err := s.store.SetClosed(ctx, pollID); err != nil {
		return httpx.Transient()
	}
	return nil
}

// Results returns the per-index tally, distinct voter total, and the caller's
// own selection. `closed` reflects an explicit close OR a passed deadline.
func (s *Service) Results(ctx context.Context, ident auth.Identity, pollID string) (Results, error) {
	p, err := s.load(ctx, pollID)
	if err != nil {
		return Results{}, err
	}
	counts, total, mine, err := s.store.Tally(ctx, pollID, p.OptionCount, ident.UserID)
	if err != nil {
		return Results{}, httpx.Transient()
	}
	return Results{
		PollID: p.ID, Closed: s.isClosed(p), OptionCount: p.OptionCount, Multi: p.Multi,
		TotalVoters: total, Tallies: counts, MyVotes: mine,
	}, nil
}

func (s *Service) load(ctx context.Context, pollID string) (Poll, error) {
	p, err := s.store.Get(ctx, pollID)
	if errors.Is(err, ErrNotFound) {
		return Poll{}, httpx.Reject(http.StatusNotFound, "POLL_NOT_FOUND", "poll not found")
	}
	if err != nil {
		return Poll{}, httpx.Transient()
	}
	return p, nil
}

func (s *Service) isClosed(p Poll) bool {
	return p.Closed || (p.ClosesAt != nil && s.now().After(*p.ClosesAt))
}
