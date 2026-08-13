package analytics

import (
	"context"

	"go.opentelemetry.io/otel/metric"
)

// Metrics are the Prometheus instruments the product dashboard renders
// (ops/dashboards/product.json): signups (a counter — the exporter appends the
// _total suffix), DAU/MAU (gauges), and the crash-free-session ratio (a gauge
// fed by crash reporting, T4.03). All aggregate — none carries a user label.
type Metrics struct {
	signups   metric.Int64Counter
	dau       metric.Int64Gauge
	mau       metric.Int64Gauge
	crashFree metric.Float64Gauge
}

// NewMetrics registers the product instruments on the given meter.
func NewMetrics(m metric.Meter) (*Metrics, error) {
	signups, err := m.Int64Counter("product_signups", metric.WithDescription("new account registrations"))
	if err != nil {
		return nil, err
	}
	dau, err := m.Int64Gauge("product_dau", metric.WithDescription("distinct daily active users"))
	if err != nil {
		return nil, err
	}
	mau, err := m.Int64Gauge("product_mau", metric.WithDescription("distinct 30-day active users"))
	if err != nil {
		return nil, err
	}
	crashFree, err := m.Float64Gauge("product_crash_free_ratio", metric.WithDescription("fraction of client sessions without a crash"))
	if err != nil {
		return nil, err
	}
	return &Metrics{signups: signups, dau: dau, mau: mau, crashFree: crashFree}, nil
}

// The setters tolerate a nil *Metrics so the service runs without a meter (tests).
func (m *Metrics) addSignups(ctx context.Context, n int64) {
	if m != nil {
		m.signups.Add(ctx, n)
	}
}

func (m *Metrics) setDAU(ctx context.Context, n int64) {
	if m != nil {
		m.dau.Record(ctx, n)
	}
}

func (m *Metrics) setMAU(ctx context.Context, n int64) {
	if m != nil {
		m.mau.Record(ctx, n)
	}
}

// SetCrashFreeRatio publishes the crash-free-session gauge (0..1).
func (m *Metrics) SetCrashFreeRatio(ctx context.Context, ratio float64) {
	if m != nil {
		m.crashFree.Record(ctx, ratio)
	}
}
