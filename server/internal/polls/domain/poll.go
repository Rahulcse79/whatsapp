// Package domain holds polls' pure logic: option-count bounds, vote validation,
// and tallying. No I/O. Polls are E2EE: the question and option TEXTS ride the
// sealed message body (the server never sees them). This context stores only
// lifecycle metadata (option COUNT, open/closed, creator) and votes BY INDEX —
// the server learns the per-index distribution but never what an option means
// (a metadata-only compromise, consistent with the project's stance).
package domain

import "errors"

const (
	// MinOptions/MaxOptions bound a poll's choices.
	MinOptions = 2
	MaxOptions = 12
)

var (
	ErrOptionCount  = errors.New("polls: options must be between 2 and 12")
	ErrClosed       = errors.New("polls: poll is closed")
	ErrBadIndex     = errors.New("polls: option index out of range")
	ErrDupIndex     = errors.New("polls: duplicate option index")
	ErrSingleChoice = errors.New("polls: this poll accepts exactly one option")
	ErrNoVote       = errors.New("polls: at least one option must be selected")
)

// ValidateCreate checks a new poll's option count.
func ValidateCreate(optionCount int) error {
	if optionCount < MinOptions || optionCount > MaxOptions {
		return ErrOptionCount
	}
	return nil
}

// ValidateVote checks a voter's chosen indices against the poll's shape: at
// least one, exactly one unless multi, all in range, no duplicates.
func ValidateVote(indices []int, optionCount int, multi bool) error {
	if len(indices) == 0 {
		return ErrNoVote
	}
	if !multi && len(indices) != 1 {
		return ErrSingleChoice
	}
	seen := make(map[int]bool, len(indices))
	for _, i := range indices {
		if i < 0 || i >= optionCount {
			return ErrBadIndex
		}
		if seen[i] {
			return ErrDupIndex
		}
		seen[i] = true
	}
	return nil
}

// Tally counts votes per option index from flat index rows (each row = one
// voter's pick of that index). The result has length optionCount.
func Tally(indices []int, optionCount int) []int {
	counts := make([]int, optionCount)
	for _, i := range indices {
		if i >= 0 && i < optionCount {
			counts[i]++
		}
	}
	return counts
}
