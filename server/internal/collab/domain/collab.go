// Package domain holds the collaboration pure logic (T12.01): validation, the
// note-version / optimistic-concurrency rule (a lightweight OT — a stale base
// version is rejected so the client rebases), and the approval state machine. No
// I/O. Full CRDT convergence for concurrent character edits is a documented seam;
// the version-gated edit model is the substance here.
package domain

import (
	"errors"
	"strings"
)

const (
	MaxTitle   = 200
	MaxBody    = 20_000
	MaxTask    = 500
	MaxComment = 2_000
)

var (
	ErrBadTitle    = errors.New("collab: title is required (max 200 chars)")
	ErrBadBody     = errors.New("collab: body too long (max 20000 chars)")
	ErrBadTask     = errors.New("collab: task title is required (max 500 chars)")
	ErrBadComment  = errors.New("collab: comment is required (max 2000 chars)")
	ErrStale       = errors.New("collab: your copy is out of date — reload and re-apply")
	ErrBadApproval = errors.New("collab: invalid approval transition")
)

// ValidateNote checks a note's title + body.
func ValidateNote(title, body string) error {
	if strings.TrimSpace(title) == "" || len(title) > MaxTitle {
		return ErrBadTitle
	}
	if len(body) > MaxBody {
		return ErrBadBody
	}
	return nil
}

// ValidateTask checks a task title.
func ValidateTask(title string) error {
	if strings.TrimSpace(title) == "" || len(title) > MaxTask {
		return ErrBadTask
	}
	return nil
}

// ValidateComment checks a comment body.
func ValidateComment(body string) error {
	if strings.TrimSpace(body) == "" || len(body) > MaxComment {
		return ErrBadComment
	}
	return nil
}

// CheckVersion enforces optimistic concurrency: an edit must be based on the
// note's current version, else it's stale (409). Returns the next version.
func CheckVersion(current, base int) (int, error) {
	if base != current {
		return 0, ErrStale
	}
	return current + 1, nil
}

// ── approval state machine ───────────────────────────────────────────────────

type ApprovalState int16

const (
	ApprovalNone     ApprovalState = 0
	ApprovalPending  ApprovalState = 1
	ApprovalApproved ApprovalState = 2
	ApprovalRejected ApprovalState = 3
)

func (s ApprovalState) String() string {
	switch s {
	case ApprovalPending:
		return "pending"
	case ApprovalApproved:
		return "approved"
	case ApprovalRejected:
		return "rejected"
	default:
		return "none"
	}
}

// CanRequestApproval: only a note with no live request (none / previously
// rejected) can be submitted for approval.
func CanRequestApproval(s ApprovalState) bool {
	return s == ApprovalNone || s == ApprovalRejected
}

// DecideApproval transitions a pending note to approved/rejected.
func DecideApproval(s ApprovalState, approve bool) (ApprovalState, error) {
	if s != ApprovalPending {
		return s, ErrBadApproval
	}
	if approve {
		return ApprovalApproved, nil
	}
	return ApprovalRejected, nil
}
