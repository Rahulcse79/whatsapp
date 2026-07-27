package presence

import (
	"fmt"
	"testing"
)

func TestSubscriptions_AddRemoveReported(t *testing.T) {
	s := NewSubscriptions()
	added, removed := s.Apply([]string{"a", "b", "a"}, nil) // "a" twice → added once
	if len(added) != 2 || len(removed) != 0 {
		t.Fatalf("added=%v removed=%v, want 2/0", added, removed)
	}
	if !s.Has("a") || !s.Has("b") {
		t.Fatal("subscribed users not tracked")
	}
	added, removed = s.Apply([]string{"b", "c"}, []string{"a"}) // b already in, c new, a out
	if len(added) != 1 || added[0] != "c" {
		t.Fatalf("added=%v, want [c]", added)
	}
	if len(removed) != 1 || removed[0] != "a" {
		t.Fatalf("removed=%v, want [a]", removed)
	}
	if s.Has("a") {
		t.Fatal("unsubscribed user still tracked")
	}
}

// The core guarantee: an unsubscribed user is never reported as subscribed, so
// the gateway filter drops their updates.
func TestSubscriptions_UnsubscribedNeverHas(t *testing.T) {
	s := NewSubscriptions()
	s.Apply([]string{"a"}, nil)
	if s.Has("z") {
		t.Fatal("never-subscribed user reported present")
	}
	s.Apply(nil, []string{"a"})
	if s.Has("a") {
		t.Fatal("unsubscribed user still reported present")
	}
}

func TestSubscriptions_CapEnforced(t *testing.T) {
	s := NewSubscriptions()
	many := make([]string, MaxSubscriptions+10)
	for i := range many {
		many[i] = fmt.Sprintf("u%d", i)
	}
	added, _ := s.Apply(many, nil)
	if len(added) != MaxSubscriptions || s.Count() != MaxSubscriptions {
		t.Fatalf("cap not enforced: added=%d count=%d, want %d", len(added), s.Count(), MaxSubscriptions)
	}
	// Over-cap users are simply not tracked.
	if s.Has("u" + fmt.Sprint(MaxSubscriptions+5)) {
		t.Fatal("over-cap user was tracked")
	}
}

func TestSubscriptions_UnsubscribeFreesRoomSameBatch(t *testing.T) {
	s := NewSubscriptions()
	many := make([]string, MaxSubscriptions)
	for i := range many {
		many[i] = fmt.Sprintf("u%d", i)
	}
	s.Apply(many, nil) // full
	// Same batch: drop u0, add "new" — the unsubscribe is processed first.
	added, removed := s.Apply([]string{"new"}, []string{"u0"})
	if len(removed) != 1 || len(added) != 1 || added[0] != "new" {
		t.Fatalf("batch add-after-remove failed: added=%v removed=%v", added, removed)
	}
	if !s.Has("new") || s.Has("u0") {
		t.Fatal("set state wrong after add-after-remove batch")
	}
}

func TestSubscriptions_Clear(t *testing.T) {
	s := NewSubscriptions()
	s.Apply([]string{"a", "b", "c"}, nil)
	cleared := s.Clear()
	if len(cleared) != 3 || s.Count() != 0 {
		t.Fatalf("clear returned %d, count now %d", len(cleared), s.Count())
	}
}
