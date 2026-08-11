// Package observability wires the OpenTelemetry SDK for every deployable
// (monitoring-logging-tracing.md §1): pull-based Prometheus metrics on
// /metrics, push-based OTLP traces to the collector when configured, W3C
// context propagation (HTTP, gRPC, and NATS headers), and RED HTTP middleware.
// Metrics are always available (Prometheus scrape); traces export only when
// WA_OTEL_ENDPOINT is set, so nothing here needs a network at startup.
package observability

import (
	"context"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
)

// Config is the per-deployable telemetry setup.
type Config struct {
	ServiceName    string
	ServiceVersion string
	Env            string
	// OTLPEndpoint is the OTLP/gRPC collector host:port; empty disables trace
	// export (spans are still created but dropped — metrics are unaffected).
	OTLPEndpoint string
}

// Telemetry holds the configured providers and the /metrics handler.
type Telemetry struct {
	Meter  metric.Meter
	Tracer trace.Tracer

	reg *prometheus.Registry
	mp  *sdkmetric.MeterProvider
	tp  *sdktrace.TracerProvider
}

// Init configures the global OTel providers + propagator and returns the
// handle. Call Shutdown on exit to flush.
func Init(ctx context.Context, cfg Config) (*Telemetry, error) {
	res, err := resource.New(ctx, resource.WithAttributes(
		attribute.String("service.name", cfg.ServiceName),
		attribute.String("service.version", cfg.ServiceVersion),
		attribute.String("deployment.environment", cfg.Env),
	))
	if err != nil {
		return nil, err
	}

	reg := prometheus.NewRegistry()
	reader, err := otelprom.New(otelprom.WithRegisterer(reg))
	if err != nil {
		return nil, err
	}
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithResource(res), sdkmetric.WithReader(reader))

	// Head sampler keeps everything so the collector can tail-sample (keep all
	// errors + slow + 1% baseline, §1); NeverSample when export is off.
	sampler := sdktrace.ParentBased(sdktrace.NeverSample())
	tpOpts := []sdktrace.TracerProviderOption{sdktrace.WithResource(res)}
	if cfg.OTLPEndpoint != "" {
		exp, err := otlptracegrpc.New(ctx,
			otlptracegrpc.WithEndpoint(cfg.OTLPEndpoint),
			otlptracegrpc.WithInsecure(),
		)
		if err != nil {
			return nil, err
		}
		sampler = sdktrace.ParentBased(sdktrace.AlwaysSample())
		tpOpts = append(tpOpts, sdktrace.WithBatcher(exp))
	}
	tpOpts = append(tpOpts, sdktrace.WithSampler(sampler))
	tp := sdktrace.NewTracerProvider(tpOpts...)

	otel.SetMeterProvider(mp)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{},
	))

	return &Telemetry{
		Meter:  mp.Meter(cfg.ServiceName),
		Tracer: tp.Tracer(cfg.ServiceName),
		reg:    reg,
		mp:     mp,
		tp:     tp,
	}, nil
}

// MetricsHandler serves the Prometheus exposition for /metrics.
func (t *Telemetry) MetricsHandler() http.Handler {
	return promhttp.HandlerFor(t.reg, promhttp.HandlerOpts{})
}

// Shutdown flushes traces and metrics (best-effort).
func (t *Telemetry) Shutdown(ctx context.Context) error {
	err := t.tp.Shutdown(ctx)
	if mErr := t.mp.Shutdown(ctx); mErr != nil && err == nil {
		err = mErr
	}
	return err
}

// WrapHTTPHandler adds server-side trace spans (W3C tracecontext extract).
func WrapHTTPHandler(h http.Handler, operation string) http.Handler {
	return otelhttp.NewHandler(h, operation)
}

// GRPCServerOption instruments a gRPC server with tracing + metrics.
func GRPCServerOption() grpc.ServerOption {
	return grpc.StatsHandler(otelgrpc.NewServerHandler())
}
