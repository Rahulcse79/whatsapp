package domain

import (
	"errors"
	"testing"
)

func TestNext_RingingTransitions(t *testing.T) {
	cases := []struct {
		ev   Event
		want RingState
	}{
		{EventAnswer, StateAnswered},
		{EventDecline, StateDeclined},
		{EventBusy, StateBusy},
		{EventMiss, StateMissed},
		{EventCancel, StateEnded},
	}
	for _, c := range cases {
		got, ok := Next(StateRinging, c.ev)
		if !ok || got != c.want {
			t.Fatalf("ringing +%d → (%d,%v), want (%d,true)", c.ev, got, ok, c.want)
		}
	}
}

func TestNext_AnsweredOnlyHangsUp(t *testing.T) {
	if got, ok := Next(StateAnswered, EventHangup); !ok || got != StateEnded {
		t.Fatalf("answered+hangup → (%d,%v), want (ended,true)", got, ok)
	}
	// Answering an already-answered call is not a transition.
	if _, ok := Next(StateAnswered, EventAnswer); ok {
		t.Fatal("answered+answer must not transition")
	}
}

func TestNext_TerminalStatesAreFrozen(t *testing.T) {
	for _, s := range []RingState{StateDeclined, StateBusy, StateMissed, StateEnded} {
		if !s.Terminal() {
			t.Fatalf("state %d should be terminal", s)
		}
		for _, ev := range []Event{EventAnswer, EventDecline, EventBusy, EventMiss, EventCancel, EventHangup} {
			if got, ok := Next(s, ev); ok || got != s {
				t.Fatalf("terminal %d +%d changed to (%d,%v)", s, ev, got, ok)
			}
		}
	}
}

func TestValidateCreate(t *testing.T) {
	if err := ValidateCreate(KindVoice, 1); err != nil {
		t.Fatalf("voice/1 callee: %v", err)
	}
	if err := ValidateCreate(KindVideo, MaxCallees); err != nil {
		t.Fatalf("video/max callees: %v", err)
	}
	if err := ValidateCreate(CallKind(9), 1); !errors.Is(err, ErrBadKind) {
		t.Fatalf("bad kind → %v, want ErrBadKind", err)
	}
	if err := ValidateCreate(KindVoice, 0); !errors.Is(err, ErrNoCallees) {
		t.Fatalf("no callees → %v, want ErrNoCallees", err)
	}
	if err := ValidateCreate(KindVoice, MaxCallees+1); !errors.Is(err, ErrTooManyCallees) {
		t.Fatalf("too many → %v, want ErrTooManyCallees", err)
	}
}
