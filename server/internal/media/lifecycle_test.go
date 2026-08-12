package media

import (
	"context"
	"errors"
	"testing"
)

type fakeDereferencer struct {
	calls []string
	err   error
}

func (f *fakeDereferencer) DecRef(_ context.Context, objectKey string) error {
	f.calls = append(f.calls, objectKey)
	return f.err
}

type fakeDeduper struct {
	seen map[string]bool
	err  error
}

func newFakeDeduper() *fakeDeduper { return &fakeDeduper{seen: map[string]bool{}} }

func (d *fakeDeduper) Seen(_ context.Context, eventID string) (bool, error) {
	if d.err != nil {
		return false, d.err
	}
	if d.seen[eventID] {
		return true, nil
	}
	d.seen[eventID] = true
	return false, nil
}

func TestLifecycle_DereferenceDecrements(t *testing.T) {
	refs := &fakeDereferencer{}
	c := NewLifecycleConsumer(refs, nil, testLog())
	if err := c.Handle(context.Background(), LifecycleEvent{Kind: LifecycleDereference, ObjectKey: "u1/o1"}); err != nil {
		t.Fatal(err)
	}
	if len(refs.calls) != 1 || refs.calls[0] != "u1/o1" {
		t.Fatalf("DecRef calls = %v, want [u1/o1]", refs.calls)
	}
}

func TestLifecycle_IgnoresOwnFactsAndUnknown(t *testing.T) {
	refs := &fakeDereferencer{}
	c := NewLifecycleConsumer(refs, nil, testLog())
	for _, kind := range []string{LifecycleUploaded, LifecycleRefAdded, LifecycleRefRemoved, LifecycleOrphaned, "totally-unknown"} {
		if err := c.Handle(context.Background(), LifecycleEvent{Kind: kind, ObjectKey: "u1/o1"}); err != nil {
			t.Fatalf("kind %q returned error: %v", kind, err)
		}
	}
	if len(refs.calls) != 0 {
		t.Fatalf("own facts must not decrement (calls=%v)", refs.calls)
	}
}

func TestLifecycle_EmptyObjectKeyIsNoop(t *testing.T) {
	refs := &fakeDereferencer{}
	c := NewLifecycleConsumer(refs, nil, testLog())
	if err := c.Handle(context.Background(), LifecycleEvent{Kind: LifecycleDereference, ObjectKey: ""}); err != nil {
		t.Fatal(err)
	}
	if len(refs.calls) != 0 {
		t.Fatal("empty object_key must not decrement")
	}
}

func TestLifecycle_DedupeDropsDuplicate(t *testing.T) {
	refs := &fakeDereferencer{}
	c := NewLifecycleConsumer(refs, newFakeDeduper(), testLog())
	ev := LifecycleEvent{Kind: LifecycleDereference, ObjectKey: "u1/o1", EventID: "evt-1"}

	if err := c.Handle(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	if err := c.Handle(context.Background(), ev); err != nil { // duplicate delivery
		t.Fatal(err)
	}
	if len(refs.calls) != 1 {
		t.Fatalf("duplicate event decremented %d times, want 1", len(refs.calls))
	}
}

func TestLifecycle_DedupeBackendErrorIsRetryable(t *testing.T) {
	refs := &fakeDereferencer{}
	dd := newFakeDeduper()
	dd.err = errors.New("valkey down")
	c := NewLifecycleConsumer(refs, dd, testLog())

	err := c.Handle(context.Background(), LifecycleEvent{Kind: LifecycleDereference, ObjectKey: "u1/o1", EventID: "evt-1"})
	if err == nil {
		t.Fatal("a dedupe backend error must surface so the event can be retried")
	}
	if len(refs.calls) != 0 {
		t.Fatal("must not decrement when dedupe state is unknown")
	}
}

func TestLifecycle_DecRefErrorPropagates(t *testing.T) {
	refs := &fakeDereferencer{err: errors.New("db down")}
	c := NewLifecycleConsumer(refs, nil, testLog())
	if err := c.Handle(context.Background(), LifecycleEvent{Kind: LifecycleDereference, ObjectKey: "u1/o1"}); err == nil {
		t.Fatal("a transient DecRef error must propagate for redelivery")
	}
}
