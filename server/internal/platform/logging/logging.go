// Package logging builds the structured JSON logger every deployable uses.
// Output conforms to the binding log schema in
// Docs/09-observability/monitoring-logging-tracing.md §2.
//
// Banned from log fields by that schema (lint-enforced later, reviewed
// always): message content or ciphertext, phone numbers, tokens/JWTs, key
// material, push tokens. Opaque UUIDs (user_id, device_id, conversation_id)
// are allowed — they are required for operations.
package logging

import (
	"context"
	"log/slog"
	"os"
	"strings"
)

// New returns the service logger: JSON to stdout, "ts" timestamp key,
// a `service` attribute on every line. Unknown levels fall back to info —
// a pod must never fail to boot over a log-level typo.
func New(service, level string) *slog.Logger {
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(strings.ToUpper(level))); err != nil {
		lvl = slog.LevelInfo
	}
	h := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: lvl,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if len(groups) == 0 && a.Key == slog.TimeKey {
				a.Key = "ts"
			}
			return a
		},
	})
	return slog.New(h).With(slog.String("service", service))
}

type traceKey struct{}

// ContextWithTraceID stores the request/frame trace id for log correlation.
// The OTel middleware (T0.23) becomes the writer of this value; handlers
// only ever read it via WithTrace.
func ContextWithTraceID(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, traceKey{}, traceID)
}

// TraceID returns the trace id stored in ctx, or "".
func TraceID(ctx context.Context) string {
	v, _ := ctx.Value(traceKey{}).(string)
	return v
}

// WithTrace returns l with the ctx trace id attached (no-op without one).
// Use at the outermost handler — the single place a request logs its outcome
// (design-patterns doc §5).
func WithTrace(ctx context.Context, l *slog.Logger) *slog.Logger {
	if id := TraceID(ctx); id != "" {
		return l.With(slog.String("trace_id", id))
	}
	return l
}
