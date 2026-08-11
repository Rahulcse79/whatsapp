package media

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/whatsapp-v2/server/internal/media/domain"
)

func testLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestGC_SweepDeletesUnreferenced(t *testing.T) {
	h := newHarness()
	_ = h.store.CreatePending(context.Background(), Object{ID: "o1", ObjectKey: "u1/o1", State: domain.StatePending})
	_ = h.store.CreatePending(context.Background(), Object{ID: "o2", ObjectKey: "u1/o2", State: domain.StateComplete, Refcount: 1})

	gc := NewGC(h.store, h.objects, h.events, testLog())
	n, err := gc.Sweep(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("deleted %d, want 1 (only the unreferenced object)", n)
	}
	if _, ok := h.store.byID["o1"]; ok {
		t.Fatal("unreferenced candidate not deleted")
	}
	if _, ok := h.store.byID["o2"]; !ok {
		t.Fatal("referenced object must not be swept")
	}
	if len(h.objects.removed) != 1 || len(h.events.orphaned) != 1 {
		t.Fatalf("expected 1 blob remove + 1 orphaned event, got removed=%d orphaned=%d",
			len(h.objects.removed), len(h.events.orphaned))
	}
}
