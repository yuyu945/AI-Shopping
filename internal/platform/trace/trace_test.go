package trace_test

import (
	"context"
	"regexp"
	"testing"

	"github.com/yuyu945/AI-Shopping/internal/platform/trace"
)

func TestWithTraceIDMakesIDAvailable(t *testing.T) {
	ctx := trace.WithTraceID(context.Background(), "request-123")
	if got := trace.TraceID(ctx); got != "request-123" {
		t.Errorf("TraceID() = %q, want request-123", got)
	}
}

func TestEnsureTraceIDPreservesExistingID(t *testing.T) {
	ctx := trace.WithTraceID(context.Background(), "request-123")
	ensured, id := trace.EnsureTraceID(ctx)

	if id != "request-123" {
		t.Errorf("EnsureTraceID() id = %q, want request-123", id)
	}
	if got := trace.TraceID(ensured); got != "request-123" {
		t.Errorf("TraceID(ensured) = %q, want request-123", got)
	}
}

func TestEnsureTraceIDGeneratesSafeNonEmptyID(t *testing.T) {
	ctx, id := trace.EnsureTraceID(context.Background())

	if id == "" {
		t.Fatal("EnsureTraceID() id is empty")
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_-]+$`).MatchString(id) {
		t.Errorf("EnsureTraceID() id = %q, want URL-safe characters", id)
	}
	if got := trace.TraceID(ctx); got != id {
		t.Errorf("TraceID(ctx) = %q, want %q", got, id)
	}
}
