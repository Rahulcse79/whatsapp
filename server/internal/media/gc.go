package media

import (
	"context"
	"log/slog"
	"time"
)

// GC sweeps unreferenced/expired objects (media-svc-lld §4). Run as a
// leader-elected singleton (K8s Lease); the sweep is idempotent, so a lease
// handoff or crash mid-sweep resumes cleanly on the next tick.
type GC struct {
	store   Store
	objects Objects
	events  Events
	log     *slog.Logger
	now     func() time.Time
}

func NewGC(store Store, objects Objects, events Events, log *slog.Logger) *GC {
	return &GC{store: store, objects: objects, events: events, log: log, now: time.Now}
}

// Sweep removes up to `limit` GC candidates (refcount 0 AND expired/pending-
// stale) and returns how many rows were deleted. Deletes are idempotent.
func (g *GC) Sweep(ctx context.Context, limit int) (int, error) {
	candidates, err := g.store.SweepCandidates(ctx, g.now(), limit)
	if err != nil {
		return 0, err
	}
	deleted := 0
	for _, o := range candidates {
		if err := g.objects.Remove(ctx, o.ObjectKey); err != nil {
			g.log.Warn("gc: object remove failed (retry next tick)", "key", o.ObjectKey, "err", err)
			continue
		}
		if err := g.store.Delete(ctx, o.ID); err != nil {
			g.log.Warn("gc: row delete failed", "id", o.ID, "err", err)
			continue
		}
		g.events.Orphaned(ctx, o.ObjectKey)
		deleted++
	}
	return deleted, nil
}

// Run sweeps on `interval` until ctx is cancelled — call only on the leader.
func (g *GC) Run(ctx context.Context, interval time.Duration, batch int) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if n, err := g.Sweep(ctx, batch); err != nil {
				g.log.Error("gc sweep failed", "err", err)
			} else if n > 0 {
				g.log.Info("gc sweep", "deleted", n)
			}
		}
	}
}
