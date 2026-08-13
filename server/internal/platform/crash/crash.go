package crash

import (
	"context"
	"sync"
)

// Transport ships a scrubbed crash payload to the self-hosted backend
// (GlitchTip/Sentry). It is the ops seam — the real HTTP/SDK transport is wired
// per deployment from WA_SENTRY_DSN; the default is a no-op.
type Transport interface {
	Send(ctx context.Context, message string, tags map[string]string)
}

// Reporter captures an error to the crash backend. Server-side reporting mirrors
// the clients' SDK-layer scrubbing (HLD §18.1): nothing leaves un-redacted.
type Reporter interface {
	Capture(ctx context.Context, err error, tags map[string]string)
}

// NoopReporter drops everything — the default when no DSN is configured.
type NoopReporter struct{}

func (NoopReporter) Capture(context.Context, error, map[string]string) {}

// ScrubbingReporter redacts the message and tags before handing them to the
// transport, so PII never crosses the process boundary.
type ScrubbingReporter struct {
	scrub *Scrubber
	next  Transport
}

func NewReporter(next Transport) *ScrubbingReporter {
	return &ScrubbingReporter{scrub: NewScrubber(), next: next}
}

func (r *ScrubbingReporter) Capture(ctx context.Context, err error, tags map[string]string) {
	if err == nil || r.next == nil {
		return
	}
	r.next.Send(ctx, r.scrub.Text(err.Error()), r.scrub.Tags(tags))
}

// CrashFreeTracker computes the crash-free-session ratio (product_crash_free_ratio)
// from counts reported by client health pings. It is a running total, safe for
// concurrent updates.
type CrashFreeTracker struct {
	mu       sync.Mutex
	sessions int64
	crashed  int64
}

func NewCrashFreeTracker() *CrashFreeTracker { return &CrashFreeTracker{} }

// Observe folds in a batch of client-reported session outcomes. crashed must
// never exceed sessions; a caller that violates that is clamped.
func (t *CrashFreeTracker) Observe(sessions, crashed int64) {
	if sessions < 0 {
		sessions = 0
	}
	if crashed < 0 {
		crashed = 0
	}
	if crashed > sessions {
		crashed = sessions
	}
	t.mu.Lock()
	t.sessions += sessions
	t.crashed += crashed
	t.mu.Unlock()
}

// Ratio is the fraction of sessions without a crash, in [0,1]. With no data yet
// it is 1.0 (nothing has crashed) so a fresh deploy doesn't read as an outage.
func (t *CrashFreeTracker) Ratio() float64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.sessions == 0 {
		return 1.0
	}
	return float64(t.sessions-t.crashed) / float64(t.sessions)
}
