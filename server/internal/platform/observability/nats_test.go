package observability

import (
	"context"
	"testing"

	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

func TestNATSPropagationRoundTrip(t *testing.T) {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{},
	))

	// A context carrying a known remote trace.
	const traceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	ctx := otel.GetTextMapPropagator().Extract(context.Background(), propagation.MapCarrier{
		"traceparent": "00-" + traceID + "-00f067aa0ba902b7-01",
	})

	// Inject into a NATS header (as a publisher would), then extract back.
	h := nats.Header{}
	InjectNATS(ctx, h)
	if h.Get("traceparent") == "" {
		t.Fatal("traceparent not injected into the NATS header")
	}

	got := trace.SpanContextFromContext(ExtractNATS(context.Background(), h))
	if got.TraceID().String() != traceID {
		t.Fatalf("trace id not propagated: got %s want %s", got.TraceID(), traceID)
	}
}
