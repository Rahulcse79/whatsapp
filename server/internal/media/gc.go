package media

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/whatsapp-v2/server/internal/media/domain"
)

// GC sweeps unreferenced/expired objects (media-svc-lld §4). Run as a
// leader-elected singleton (K8s Lease); the sweep is idempotent, so a lease
// handoff or crash mid-sweep resumes cleanly on the next tick.
type GC struct {
	store    Store
	objects  Objects
	sessions Sessions
	events   Events
	log      *slog.Logger
	now      func() time.Time
}

func NewGC(store Store, objects Objects, sessions Sessions, events Events, log *slog.Logger) *GC {
	return &GC{store: store, objects: objects, sessions: sessions, events: events, log: log, now: time.Now}
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
		if err := g.reclaim(ctx, o); err != nil {
			g.log.Warn("gc: storage reclaim failed (retry next tick)", "key", o.ObjectKey, "id", o.ID, "err", err)
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

// reclaim releases an object's underlying storage. The two GC-candidate shapes
// need different cleanup (media-svc-lld §2):
//
//   - a PENDING upload never completed, so there is no object to delete — we
//     ABORT its in-progress multipart. If the session (which holds the MinIO
//     upload handle) has already aged out of its 24 h TTL, MinIO's
//     AbortIncompleteMultipartUpload lifecycle rule is the backstop.
//   - a COMPLETE object is removed outright.
//
// Both are idempotent, so a redelivered sweep is harmless.
func (g *GC) reclaim(ctx context.Context, o Object) error {
	if o.State == domain.StatePending {
		sess, err := g.sessions.Load(ctx, o.ID)
		if errors.Is(err, ErrNotFound) {
			return nil // handle gone; leave the incomplete multipart to MinIO ILM
		}
		if err != nil {
			return err
		}
		if err := g.objects.Abort(ctx, sess.ObjectKey, sess.Handle); err != nil {
			return err
		}
		_ = g.sessions.Delete(ctx, o.ID)
		return nil
	}
	return g.objects.Remove(ctx, o.ObjectKey)
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
