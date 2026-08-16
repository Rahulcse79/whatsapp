// Package domain holds the anti-abuse pure logic (T10.03): report-reason
// validation and the report rate-limit shape. The spam/phishing/scam heuristics
// themselves run on-device (E2EE — the server never sees message content); the
// server only files metadata reports into the trust-and-safety queue (T4.01) and
// rate-limits abuse of the report path itself.
package domain

import (
	"errors"
	"strings"
)

// Reason is why a user was reported (reports.reason smallint).
type Reason int16

const (
	ReasonSpam          Reason = 0
	ReasonHarassment    Reason = 1
	ReasonScam          Reason = 2 // scam / phishing
	ReasonImpersonation Reason = 3
	ReasonOther         Reason = 4
)

const MaxNote = 500

var (
	ErrBadReason = errors.New("abuse: unknown report reason")
	ErrBadNote   = errors.New("abuse: note too long (max 500 chars)")
	ErrSelf      = errors.New("abuse: you can't report yourself")
)

func (r Reason) Valid() bool { return r >= ReasonSpam && r <= ReasonOther }

func (r Reason) String() string {
	switch r {
	case ReasonHarassment:
		return "harassment"
	case ReasonScam:
		return "scam"
	case ReasonImpersonation:
		return "impersonation"
	case ReasonOther:
		return "other"
	default:
		return "spam"
	}
}

// ValidateReport checks a report submission.
func ValidateReport(reason Reason, note string) error {
	if !reason.Valid() {
		return ErrBadReason
	}
	if len(note) > MaxNote {
		return ErrBadNote
	}
	return nil
}

// NormalizeNote trims a report note.
func NormalizeNote(note string) string { return strings.TrimSpace(note) }
