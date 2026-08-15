// Package polls owns poll lifecycle metadata + index-based vote tallying. Poll
// content (question, option texts) is E2EE and never transits here — the client
// seals it in the message body; this context stores the option COUNT, open/closed
// state, and votes by option INDEX (T6.02). Create registers a poll (the client
// then sends the sealed poll message referencing poll_id); Vote/Close/Results
// drive the rest.
package polls

import (
	"context"
	"errors"
	"time"
)

// ErrNotFound is returned when no poll matches.
var ErrNotFound = errors.New("polls: not found")

// Poll is a poll's server-visible metadata (no content).
type Poll struct {
	ID             string
	ConversationID string
	CreatorID      string
	OptionCount    int
	Multi          bool
	Closed         bool
	ClosesAt       *time.Time // optional auto-close deadline
	CreatedAt      time.Time
}

// CreateResult is the POST /v1/polls response — the client seals a poll message
// carrying this poll_id + the (E2EE) question/options.
type CreateResult struct {
	PollID string `json:"poll_id"`
}

// Results is the GET /v1/polls/{id} response.
type Results struct {
	PollID      string `json:"poll_id"`
	Closed      bool   `json:"closed"`
	OptionCount int    `json:"option_count"`
	Multi       bool   `json:"multi"`
	TotalVoters int    `json:"total_voters"`
	Tallies     []int  `json:"tallies"`  // voters per option index
	MyVotes     []int  `json:"my_votes"` // the caller's chosen indices
}

// Store persists polls + poll_votes.
type Store interface {
	Create(ctx context.Context, p Poll) error
	Get(ctx context.Context, id string) (Poll, error) // ErrNotFound
	SetClosed(ctx context.Context, id string) error
	// ReplaceVotes atomically swaps the voter's chosen indices for the poll
	// (re-voting replaces the prior selection).
	ReplaceVotes(ctx context.Context, pollID, voterID string, indices []int) error
	// Tally returns per-index voter counts (len optionCount), the distinct voter
	// total, and the caller's own chosen indices.
	Tally(ctx context.Context, pollID string, optionCount int, voterID string) (counts []int, totalVoters int, mine []int, err error)
}
