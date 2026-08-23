package domain

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
)

// Status is where a payment sits in its lifecycle.
type Status string

const (
	// StatusPending: we created an intent with the PSP; the payer has not
	// finished (or we have not heard that they did).
	StatusPending Status = "pending"
	// StatusSucceeded: the PSP confirmed the money moved. Terminal-ish — a
	// succeeded payment may still be refunded.
	StatusSucceeded Status = "succeeded"
	// StatusFailed: the PSP declined or the payer abandoned. Terminal.
	StatusFailed Status = "failed"
	// StatusCanceled: cancelled before completion. Terminal.
	StatusCanceled Status = "canceled"
	// StatusRefunded: money returned. Terminal.
	StatusRefunded Status = "refunded"
)

// Purpose is what a payment is for. The three the product needs (T15.05).
type Purpose string

const (
	PurposePremium Purpose = "premium"      // account-level premium subscription
	PurposeChannel Purpose = "channel_sub"  // paid channel subscription
	PurposeP2P     Purpose = "p2p_transfer" // person-to-person transfer
)

var (
	ErrBadStatus     = errors.New("payments: unknown status")
	ErrBadPurpose    = errors.New("payments: unknown purpose")
	ErrBadTransition = errors.New("payments: illegal status transition")
)

// transitions is the whole state machine. Anything not listed is illegal, which
// is what stops a replayed or out-of-order webhook from resurrecting a payment
// (a PSP may deliver events more than once and out of order — both are normal).
var transitions = map[Status]map[Status]bool{
	StatusPending:   {StatusSucceeded: true, StatusFailed: true, StatusCanceled: true},
	StatusSucceeded: {StatusRefunded: true},
	StatusFailed:    {}, // terminal
	StatusCanceled:  {}, // terminal
	StatusRefunded:  {}, // terminal
}

// ParseStatus validates a status arriving from storage or a PSP mapping.
func ParseStatus(s string) (Status, error) {
	st := Status(s)
	if _, ok := transitions[st]; !ok {
		return "", ErrBadStatus
	}
	return st, nil
}

// ParsePurpose validates a purpose.
func ParsePurpose(s string) (Purpose, error) {
	switch Purpose(s) {
	case PurposePremium, PurposeChannel, PurposeP2P:
		return Purpose(s), nil
	default:
		return "", ErrBadPurpose
	}
}

// Terminal reports whether no further transition is possible.
func (s Status) Terminal() bool { return len(transitions[s]) == 0 }

// CanTransition reports whether from → to is legal.
func CanTransition(from, to Status) bool { return transitions[from][to] }

// Transition applies a status change, or explains why it cannot.
//
// Re-applying the status a payment already has is NOT an error: PSPs redeliver,
// and a webhook handler that 500s on a duplicate will be retried forever. It
// returns `changed=false` so the caller can skip the side effects.
func Transition(from, to Status) (changed bool, err error) {
	if _, ok := transitions[to]; !ok {
		return false, ErrBadStatus
	}
	if from == to {
		return false, nil // idempotent redelivery
	}
	if !CanTransition(from, to) {
		return false, ErrBadTransition
	}
	return true, nil
}

// ── PSP webhook authentication ─────────────────────────────────────────────

var ErrBadSignature = errors.New("payments: webhook signature mismatch")

// SignWebhook computes the hex HMAC-SHA256 of a raw webhook body. Every PSP
// worth using signs its callbacks; this is the reference construction for the
// self-hosted/dev adapter and the shape a real adapter maps onto.
func SignWebhook(secret, payload []byte) string {
	m := hmac.New(sha256.New, secret)
	m.Write(payload)
	return hex.EncodeToString(m.Sum(nil))
}

// VerifyWebhook constant-time checks a presented signature over the RAW body.
//
// It must be the raw bytes, not a re-serialised struct: re-encoding changes key
// order and whitespace, so the signature would never match — and a handler that
// "fixes" that by skipping verification is how forged payment events get in.
func VerifyWebhook(secret, payload []byte, signature string) error {
	if signature == "" {
		return ErrBadSignature
	}
	want := SignWebhook(secret, payload)
	if !hmac.Equal([]byte(want), []byte(signature)) {
		return ErrBadSignature
	}
	return nil
}
