package fanout

import (
	"context"
	"strconv"
	"testing"
)

type memAgg struct {
	expected map[string]int
	reached  map[string]map[string]struct{} // "msg:kind" → device set
}

func newMemAgg() *memAgg {
	return &memAgg{expected: map[string]int{}, reached: map[string]map[string]struct{}{}}
}

func (m *memAgg) SetExpected(_ context.Context, msg string, n int) error {
	m.expected[msg] = n
	return nil
}

func (m *memAgg) Reached(_ context.Context, msg string, kind int16, dev string) (int, int, bool, error) {
	key := msg + ":" + strconv.Itoa(int(kind))
	set := m.reached[key]
	if set == nil {
		set = map[string]struct{}{}
		m.reached[key] = set
	}
	_, existed := set[dev]
	if !existed {
		set[dev] = struct{}{}
	}
	return len(set), m.expected[msg], !existed, nil
}

type recEmit struct{ emits int }

func (e *recEmit) EmitAggregate(_ context.Context, _, _ string, _ int16) error {
	e.emits++
	return nil
}

func TestAggregateReceipts_FlipsOnceWhenAllReached(t *testing.T) {
	store, emit := newMemAgg(), &recEmit{}
	ar := NewAggregateReceipts(store, emit)
	ctx := context.Background()
	_ = store.SetExpected(ctx, "m1", 3)

	for _, d := range []string{"d1", "d2"} {
		if err := ar.OnDeviceReceipt(ctx, "sender", "m1", 1, d); err != nil {
			t.Fatal(err)
		}
	}
	if emit.emits != 0 {
		t.Fatalf("emitted before all devices reached: %d", emit.emits)
	}

	if err := ar.OnDeviceReceipt(ctx, "sender", "m1", 1, "d3"); err != nil { // last device → flip
		t.Fatal(err)
	}
	if emit.emits != 1 {
		t.Fatalf("aggregate tick emits = %d, want 1", emit.emits)
	}

	// Duplicate receipts must never re-emit.
	_ = ar.OnDeviceReceipt(ctx, "sender", "m1", 1, "d3")
	_ = ar.OnDeviceReceipt(ctx, "sender", "m1", 1, "d1")
	if emit.emits != 1 {
		t.Fatalf("duplicate receipts re-emitted the aggregate tick: %d", emit.emits)
	}
}

func TestAggregateReceipts_DeliveredAndReadTrackedSeparately(t *testing.T) {
	store, emit := newMemAgg(), &recEmit{}
	ar := NewAggregateReceipts(store, emit)
	ctx := context.Background()
	_ = store.SetExpected(ctx, "m1", 1)

	// delivered (kind 1) from the only device → flip
	if err := ar.OnDeviceReceipt(ctx, "sender", "m1", 1, "d1"); err != nil {
		t.Fatal(err)
	}
	// read (kind 2) from the same device → separate set → flip again
	if err := ar.OnDeviceReceipt(ctx, "sender", "m1", 2, "d1"); err != nil {
		t.Fatal(err)
	}
	if emit.emits != 2 {
		t.Fatalf("delivered + read should each flip once: emits = %d, want 2", emit.emits)
	}
}
