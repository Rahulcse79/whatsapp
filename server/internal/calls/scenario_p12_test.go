package calls

import (
	"context"
	"testing"
	"time"

	"github.com/whatsapp-v2/server/internal/calls/domain"
)

// TestScenarioP12_TransitionMatrix is the exhaustive ring-state transition matrix
// (protocol scenario P12, test-strategy §3): every (state, event) pair maps to
// the server-authoritative next state, and every event from a resolved state is a
// no-op — which is what makes duplicate requests and webhook redelivery safe.
func TestScenarioP12_TransitionMatrix(t *testing.T) {
	type want struct {
		next domain.RingState
		ok   bool
	}
	allEvents := []domain.Event{
		domain.EventAnswer, domain.EventDecline, domain.EventBusy,
		domain.EventMiss, domain.EventCancel, domain.EventHangup,
	}

	matrix := map[domain.RingState]map[domain.Event]want{
		domain.StateRinging: {
			domain.EventAnswer:  {domain.StateAnswered, true},
			domain.EventDecline: {domain.StateDeclined, true},
			domain.EventBusy:    {domain.StateBusy, true},
			domain.EventMiss:    {domain.StateMissed, true},
			domain.EventCancel:  {domain.StateEnded, true},
			domain.EventHangup:  {domain.StateRinging, false}, // hangup invalid while ringing
		},
		domain.StateAnswered: {
			domain.EventHangup:  {domain.StateEnded, true},
			domain.EventAnswer:  {domain.StateAnswered, false},
			domain.EventDecline: {domain.StateAnswered, false},
			domain.EventBusy:    {domain.StateAnswered, false},
			domain.EventMiss:    {domain.StateAnswered, false},
			domain.EventCancel:  {domain.StateAnswered, false},
		},
	}
	// Every event from a resolved (terminal or signaling-only) state is a no-op.
	for _, s := range []domain.RingState{
		domain.StateAnsweredElsewhere, domain.StateDeclined, domain.StateBusy,
		domain.StateMissed, domain.StateEnded,
	} {
		evs := make(map[domain.Event]want, len(allEvents))
		for _, ev := range allEvents {
			evs[ev] = want{s, false}
		}
		matrix[s] = evs
	}

	for state, evs := range matrix {
		for ev, w := range evs {
			if got, ok := domain.Next(state, ev); got != w.next || ok != w.ok {
				t.Errorf("Next(state=%d, event=%d) = (%d, %v), want (%d, %v)", state, ev, got, ok, w.next, w.ok)
			}
		}
	}
}

// TestScenarioP12_AnswerElsewhere: an answer resolves the ring — the caller is
// told ANSWERED, and every OTHER callee device (the answerer's siblings and other
// callees) gets ANSWERED_ELSEWHERE, never the answering device itself.
func TestScenarioP12_AnswerElsewhere(t *testing.T) {
	h := newHarness()
	res, err := h.svc.Create(context.Background(), who("caller", "cd1"), []string{"bob", "carol"}, domain.KindVideo)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.Answer(context.Background(), who("bob", "bob-d1"), res.RingID); err != nil {
		t.Fatal(err)
	}

	sawAnswered := false
	elsewhere := map[string]bool{}
	for _, r := range h.sig.rings {
		switch r.state {
		case domain.StateAnswered:
			sawAnswered = true
		case domain.StateAnsweredElsewhere:
			for _, d := range r.devices {
				elsewhere[d] = true
			}
		default:
			// other ring states aren't emitted on the answer path
		}
	}
	if !sawAnswered {
		t.Fatal("caller must be told ANSWERED")
	}
	if elsewhere["bob-d1"] {
		t.Fatal("must not send ANSWERED_ELSEWHERE to the answering device")
	}
	for _, d := range []string{"bob-d2", "carol-d1"} {
		if !elsewhere[d] {
			t.Errorf("device %s should get ANSWERED_ELSEWHERE", d)
		}
	}
	if h.ring.byRing[res.RingID].State != domain.StateAnswered {
		t.Fatal("ring should be answered")
	}
}

// TestScenarioP12_Decline: a decline resolves the ring to declined and records it.
func TestScenarioP12_Decline(t *testing.T) {
	h := newHarness()
	res, _ := h.svc.Create(context.Background(), who("caller", "cd1"), []string{"bob"}, domain.KindVoice)
	if err := h.svc.Decline(context.Background(), who("bob", "bob-d1"), res.RingID); err != nil {
		t.Fatal(err)
	}
	if h.ring.byRing[res.RingID].State != domain.StateDeclined {
		t.Fatal("ring should be declined")
	}
	if h.hist.recs[res.RingID].Outcome != OutcomeDeclined {
		t.Fatalf("outcome = %v, want declined", h.hist.recs[res.RingID].Outcome)
	}
}

// TestScenarioP12_Timeout: an unanswered ring times out to missed on the
// server-authoritative 45 s deadline, notifying the caller and pushing the callees.
func TestScenarioP12_Timeout(t *testing.T) {
	h := newHarness()
	res, _ := h.svc.Create(context.Background(), who("caller", "cd1"), []string{"bob"}, domain.KindVoice)

	h.now = h.now.Add(domain.MissedAfter + time.Second) // past the deadline
	n, err := h.svc.SweepMissed(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("swept %d rings, want 1", n)
	}
	if h.ring.byRing[res.RingID].State != domain.StateMissed {
		t.Fatal("unanswered ring should be missed")
	}
	if h.push.missed != 1 {
		t.Fatalf("missed-call pushes = %d, want 1", h.push.missed)
	}
	if h.hist.recs[res.RingID].Outcome != OutcomeMissed {
		t.Fatalf("outcome = %v, want missed", h.hist.recs[res.RingID].Outcome)
	}
}
