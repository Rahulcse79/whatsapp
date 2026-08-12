// Package domain holds call-ctl's pure logic: the server-authoritative ring
// state machine, call-kind/participant validation, and the ring timing budget.
// No I/O (depguard domain-stays-pure). State values mirror wsv1.RingState so the
// wire mapping is the identity.
package domain

import "time"

const (
	// MaxCallees bounds a call fan-out (caller + ≤ 31 callees = ≤ 32, HLD §15).
	MaxCallees = 31
	// MissedAfter is how long a ring stays unanswered before → missed.
	MissedAfter = 45 * time.Second
	// JoinTokenTTL is the LiveKit join JWT lifetime (short-lived, room-scoped).
	JoinTokenTTL = 60 * time.Second
	// RingTTL bounds how long ring state is retained (a little past the miss
	// deadline, so late transitions/webhooks still find it).
	RingTTL = 2 * time.Minute
	// CallMaxTTL retains an answered call's ring record for its lifetime, so
	// rejoin (network recovery) can re-authorize a participant.
	CallMaxTTL = 8 * time.Hour
)

// CallKind is voice or video (values match wsv1.CallKind).
type CallKind int16

const (
	KindVoice CallKind = 1
	KindVideo CallKind = 2
)

func (k CallKind) Valid() bool { return k == KindVoice || k == KindVideo }

// RingState is the server-authoritative ring status (values match wsv1.RingState).
type RingState int16

const (
	StateRinging  RingState = 1
	StateAnswered RingState = 2
	// StateAnsweredElsewhere is a per-recipient SIGNALING state only (never
	// stored): the other devices of a callee learn the call was answered on one
	// of them. Value matches wsv1.RingState.RING_STATE_ANSWERED_ELSEWHERE.
	StateAnsweredElsewhere RingState = 3
	StateDeclined          RingState = 4
	StateBusy              RingState = 5
	StateMissed            RingState = 6
	StateEnded             RingState = 7
)

// Terminal reports whether no further transition is possible from this state.
func (s RingState) Terminal() bool {
	switch s {
	case StateDeclined, StateBusy, StateMissed, StateEnded:
		return true
	default:
		return false
	}
}

// Event drives the ring machine.
type Event int

const (
	EventAnswer  Event = iota + 1 // a callee accepted
	EventDecline                  // a callee rejected
	EventBusy                     // a callee is already on a call
	EventMiss                     // the miss deadline elapsed
	EventCancel                   // the caller hung up before answer
	EventHangup                   // an answered call ended
)

// Next applies an event to the current state, returning the resulting state and
// whether it actually changed. Transitions are one-way: from a terminal state,
// or on an event the state doesn't accept, nothing changes (ok=false) — which
// makes duplicate requests and webhook redelivery idempotent.
//
//	ringing ──answer──▶ answered ──hangup──▶ ended
//	   │  ├─decline──▶ declined
//	   │  ├─busy─────▶ busy
//	   │  ├─miss─────▶ missed
//	   │  └─cancel───▶ ended
func Next(cur RingState, ev Event) (RingState, bool) {
	switch cur {
	case StateRinging:
		switch ev {
		case EventAnswer:
			return StateAnswered, true
		case EventDecline:
			return StateDeclined, true
		case EventBusy:
			return StateBusy, true
		case EventMiss:
			return StateMissed, true
		case EventCancel:
			return StateEnded, true
		default:
			// EventHangup is not valid while still ringing — no transition.
		}
	case StateAnswered:
		if ev == EventHangup {
			return StateEnded, true
		}
	default:
		// Terminal / signaling-only states (declined/busy/missed/ended/
		// answered-elsewhere) accept no further transition.
	}
	return cur, false
}

// ValidateCreate checks a new call's kind and callee fan-out.
func ValidateCreate(kind CallKind, calleeCount int) error {
	if !kind.Valid() {
		return ErrBadKind
	}
	if calleeCount == 0 {
		return ErrNoCallees
	}
	if calleeCount > MaxCallees {
		return ErrTooManyCallees
	}
	return nil
}
