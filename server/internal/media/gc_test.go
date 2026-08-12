package media

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/whatsapp-v2/server/internal/media/domain"
)

func testLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestGC_SweepReclaimsByState(t *testing.T) {
	h := newHarness()
	ctx := context.Background()

	// Complete + unreferenced → deleted via object Remove.
	_ = h.store.CreatePending(ctx, Object{ID: "o1", ObjectKey: "u1/o1", State: domain.StateComplete})
	// Complete + still referenced → must NOT be swept.
	_ = h.store.CreatePending(ctx, Object{ID: "o2", ObjectKey: "u1/o2", State: domain.StateComplete, Refcount: 1})
	// Pending-stale WITH a live session → deleted via multipart Abort.
	_ = h.store.CreatePending(ctx, Object{ID: "o3", ObjectKey: "u1/o3", State: domain.StatePending})
	_ = h.sessions.Save(ctx, UploadSession{UploadID: "o3", ObjectKey: "u1/o3", Handle: "handle-o3"}, time.Hour)
	// Pending-stale with NO session (handle aged out) → row deleted, storage left
	// to MinIO ILM (no Remove, no Abort).
	_ = h.store.CreatePending(ctx, Object{ID: "o4", ObjectKey: "u1/o4", State: domain.StatePending})

	gc := NewGC(h.store, h.objects, h.sessions, h.events, testLog())
	n, err := gc.Sweep(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}

	if n != 3 {
		t.Fatalf("deleted %d, want 3 (o1, o3, o4)", n)
	}
	if _, ok := h.store.byID["o2"]; !ok {
		t.Fatal("referenced object o2 must not be swept")
	}
	for _, id := range []string{"o1", "o3", "o4"} {
		if _, ok := h.store.byID[id]; ok {
			t.Fatalf("candidate %s row not deleted", id)
		}
	}

	if len(h.objects.removed) != 1 || h.objects.removed[0] != "u1/o1" {
		t.Fatalf("removed = %v, want [u1/o1] (only the complete object)", h.objects.removed)
	}
	if len(h.objects.aborted) != 1 || h.objects.aborted[0] != "u1/o3" {
		t.Fatalf("aborted = %v, want [u1/o3] (only the pending w/ session)", h.objects.aborted)
	}
	if _, ok := h.sessions.m["o3"]; ok {
		t.Fatal("aborted upload's session should be deleted")
	}
	if len(h.events.orphaned) != 3 {
		t.Fatalf("orphaned events = %d, want 3", len(h.events.orphaned))
	}
}
