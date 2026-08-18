// Package trace provides context-based trace ID primitives.
package trace

import (
	"context"
	"crypto/rand"
	"encoding/base64"
)

type contextKey struct{}

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

// EnsureTraceID returns ctx with its existing trace ID, or attaches a newly
// generated URL-safe trace ID when none is present.
func EnsureTraceID(ctx context.Context) (context.Context, string) {
	if traceID := TraceID(ctx); traceID != "" {
		return ctx, traceID
	}

	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		panic("crypto/rand failed to generate trace ID")
	}
	traceID := base64.RawURLEncoding.EncodeToString(bytes)
	return WithTraceID(ctx, traceID), traceID
}
