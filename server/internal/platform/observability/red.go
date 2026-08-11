package observability

import (
	"bufio"
	"errors"
	"net"
	"net/http"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// SLOBucketsSeconds are histogram boundaries aligned to the message-latency SLO
// (50/100/150/250/500/1000 ms, monitoring-logging-tracing.md §1) with finer and
// coarser context for general RED latency.
var SLOBucketsSeconds = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.15, 0.25, 0.5, 1, 2.5, 5}

// HTTPMetrics is the RED instrument set for an HTTP surface. Instruments are
// named without the Prometheus suffix (_total/_seconds); the exporter adds it.
type HTTPMetrics struct {
	requests metric.Int64Counter
	duration metric.Float64Histogram
	inflight metric.Int64UpDownCounter
}

func NewHTTPMetrics(m metric.Meter) (*HTTPMetrics, error) {
	requests, err := m.Int64Counter("http_requests",
		metric.WithDescription("HTTP requests handled (RED: rate + errors)."))
	if err != nil {
		return nil, err
	}
	duration, err := m.Float64Histogram("http_request_duration",
		metric.WithDescription("HTTP request duration (RED)."),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(SLOBucketsSeconds...))
	if err != nil {
		return nil, err
	}
	inflight, err := m.Int64UpDownCounter("http_requests_in_flight",
		metric.WithDescription("In-flight HTTP requests."))
	if err != nil {
		return nil, err
	}
	return &HTTPMetrics{requests: requests, duration: duration, inflight: inflight}, nil
}

// Middleware records rate/errors/duration per (method, route, status). The
// route label is the matched ServeMux pattern (low cardinality), read after the
// inner handler runs.
func (h *HTTPMetrics) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ctx := r.Context()
		h.inflight.Add(ctx, 1)
		defer h.inflight.Add(ctx, -1)

		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)

		route := r.Pattern
		if route == "" {
			route = "other"
		}
		attrs := metric.WithAttributes(
			attribute.String("method", r.Method),
			attribute.String("route", route),
			attribute.Int("status", sw.status),
		)
		h.requests.Add(ctx, 1, attrs)
		h.duration.Record(ctx, time.Since(start).Seconds(), attrs)
	})
}

// statusWriter captures the response status and passes through Hijack/Flush so
// the middleware can wrap WS-upgrading and streaming handlers.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hj, ok := s.ResponseWriter.(http.Hijacker); ok {
		return hj.Hijack()
	}
	return nil, nil, errors.New("observability: ResponseWriter does not support Hijack")
}

func (s *statusWriter) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
