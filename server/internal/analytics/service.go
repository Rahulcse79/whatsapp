package analytics

import (
	"context"
	"time"

	"github.com/whatsapp-v2/server/internal/analytics/domain"
)

// Service aggregates events into rollups. It is deliberately tiny: ingest routes
// each event to a counter or the distinct sketch; a periodic job condenses the
// sketch into DAU/MAU; a retention job trims old rollups.
type Service struct {
	rollups  Rollups
	distinct Distinct
	metrics  *Metrics // may be nil (no meter)
	now      func() time.Time
}

func NewService(rollups Rollups, distinct Distinct, metrics *Metrics) *Service {
	return &Service{rollups: rollups, distinct: distinct, metrics: metrics, now: time.Now}
}

// Ingest applies one event. Counter kinds add to the day's rollup cell (and, for
// signups, bump the Prometheus counter); active-user ticks feed the distinct
// sketch. Unknown or malformed events are dropped — analytics must never be a
// reason a request fails, so this is best-effort.
func (s *Service) Ingest(ctx context.Context, e Event) error {
	if !e.Kind.Known() {
		return nil
	}
	at := e.At
	if at.IsZero() {
		at = s.now()
	}
	day := domain.Day(at)

	if e.Kind.Distinct() {
		if e.UserID == "" {
			return nil
		}
		return s.distinct.Add(ctx, hllBucket(e.Kind, day), e.UserID)
	}

	metric, ok := e.Kind.CounterMetric(e.Label)
	if !ok {
		return nil
	}
	delta := e.Delta
	if delta <= 0 {
		delta = 1
	}
	if e.Kind == domain.KindSignup {
		s.metrics.addSignups(ctx, delta)
	}
	return s.rollups.IncrDaily(ctx, day, metric, delta)
}

// RollupDistinct condenses the distinct sketch into the durable DAU (today) and
// MAU (trailing 30 days) rollup cells and the Prometheus gauges. Idempotent:
// SetDaily overwrites, and the sketch only grows within a day, so re-running
// (or overlapping pods) converges to the same numbers.
func (s *Service) RollupDistinct(ctx context.Context, day time.Time) error {
	day = domain.Day(day)

	dau, err := s.distinct.Count(ctx, hllBucket(domain.KindActiveUser, day))
	if err != nil {
		return err
	}
	month := make([]string, 0, domain.MAUWindow)
	for _, d := range domain.TrailingDays(day, domain.MAUWindow) {
		month = append(month, hllBucket(domain.KindActiveUser, d))
	}
	mau, err := s.distinct.Count(ctx, month...)
	if err != nil {
		return err
	}

	if err := s.rollups.SetDaily(ctx, day, "dau", dau); err != nil {
		return err
	}
	if err := s.rollups.SetDaily(ctx, day, "mau", mau); err != nil {
		return err
	}
	s.metrics.setDAU(ctx, dau)
	s.metrics.setMAU(ctx, mau)
	return nil
}

// Query returns rollup cells in [from, to] (inclusive), for the admin/dashboard
// read path.
func (s *Service) Query(ctx context.Context, from, to time.Time) ([]DailyValue, error) {
	return s.rollups.Query(ctx, domain.Day(from), domain.Day(to))
}

// Purge trims rollups past the retention window (~13 months).
func (s *Service) Purge(ctx context.Context) (int64, error) {
	return s.rollups.PurgeOlderThan(ctx, domain.RetentionCutoff(s.now()))
}

// SetCrashFreeRatio publishes the crash-free-session gauge (fed by client health
// pings; see internal/platform/crash).
func (s *Service) SetCrashFreeRatio(ctx context.Context, ratio float64) {
	s.metrics.SetCrashFreeRatio(ctx, ratio)
}
