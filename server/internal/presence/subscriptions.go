package presence

import "sync"

// MaxSubscriptions caps how many users one device tracks at once — the
// on-screen chat window, never the whole address book (DS&A §10: presence
// fans out on subscription, avoiding O(contacts) broadcast storms).
const MaxSubscriptions = 50

// Subscriptions is a connection's set of tracked users. Subscribing to a user
// yields both their presence updates and their typing indicators (one fan-out
// dimension, matching the PresenceSub{user_ids} frame). Safe for concurrent use.
type Subscriptions struct {
	mu  sync.Mutex
	set map[string]struct{}
}

func NewSubscriptions() *Subscriptions {
	return &Subscriptions{set: make(map[string]struct{})}
}

// Apply adds and removes subscriptions, enforcing the per-device cap. It
// returns the users newly added and newly removed so the caller can open and
// close the matching fan-out subscriptions. Unsubscribes are processed first,
// so the same batch can free room for new subscriptions. Over-cap subscribe
// requests are dropped, not errored: the client re-subscribes as chats scroll
// back on screen.
func (s *Subscriptions) Apply(sub, unsub []string) (added, removed []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, u := range unsub {
		if _, ok := s.set[u]; ok {
			delete(s.set, u)
			removed = append(removed, u)
		}
	}
	for _, u := range sub {
		if u == "" {
			continue
		}
		if _, ok := s.set[u]; ok {
			continue
		}
		if len(s.set) >= MaxSubscriptions {
			continue
		}
		s.set[u] = struct{}{}
		added = append(added, u)
	}
	return added, removed
}

// Has reports whether the connection is subscribed to a user — the filter that
// guarantees unsubscribed users never receive updates.
func (s *Subscriptions) Has(userID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.set[userID]
	return ok
}

// Count returns the current subscription count.
func (s *Subscriptions) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.set)
}

// Clear drops every subscription, returning them so the caller can close the
// fan-out subscriptions on disconnect.
func (s *Subscriptions) Clear() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.set))
	for u := range s.set {
		out = append(out, u)
	}
	s.set = make(map[string]struct{})
	return out
}
