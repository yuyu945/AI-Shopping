// Package trace provides context-based trace ID primitives.
package trace

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"

	oteltrace "go.opentelemetry.io/otel/trace"
)

type contextKey struct{}

var remoteParentSpanID = oteltrace.SpanID{0, 0, 0, 0, 0, 0, 0, 1}

// WithTraceID returns a context carrying traceID.
func WithTraceID(ctx context.Context, traceID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, contextKey{}, traceID)
}

// TraceID returns the trace ID stored in ctx, or an empty string when absent.
func TraceID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	traceID, _ := ctx.Value(contextKey{}).(string)
	return traceID
}

// WithRemoteTraceID attaches a valid W3C trace ID to ctx for both the local
// request context and OpenTelemetry-based infrastructure logging.
func WithRemoteTraceID(ctx context.Context, traceID string) (context.Context, bool) {
	if ctx == nil {
		ctx = context.Background()
	}

	otelTraceID, err := oteltrace.TraceIDFromHex(traceID)
	if err != nil {
		return ctx, false
	}

	ctx = WithTraceID(ctx, otelTraceID.String())
	spanContext := oteltrace.NewSpanContext(oteltrace.SpanContextConfig{
		TraceID: otelTraceID,
		SpanID:  remoteParentSpanID,
		Remote:  true,
	})
	return oteltrace.ContextWithRemoteSpanContext(ctx, spanContext), true
}

// EnsureTraceID returns ctx with its existing trace ID, or attaches a newly
// generated URL-safe trace ID when none is present. It returns an error when
// the cryptographic random source cannot generate an ID.
func EnsureTraceID(ctx context.Context) (context.Context, string, error) {
	return EnsureTraceIDWithReader(ctx, rand.Reader)
}

// EnsureTraceIDWithReader is equivalent to EnsureTraceID, using reader as the
// random source. It is intended for integration boundaries and deterministic
// tests that need to control random-source failures.
func EnsureTraceIDWithReader(ctx context.Context, reader io.Reader) (context.Context, string, error) {
	if traceID := TraceID(ctx); traceID != "" {
		return ctx, traceID, nil
	}
	if reader == nil {
		return ctx, "", fmt.Errorf("trace ID random source is nil")
	}

	bytes := make([]byte, 16)
	if _, err := io.ReadFull(reader, bytes); err != nil {
		return ctx, "", fmt.Errorf("generate trace ID: %w", err)
	}
	traceID := base64.RawURLEncoding.EncodeToString(bytes)
	return WithTraceID(ctx, traceID), traceID, nil
}
