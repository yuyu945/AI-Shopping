package trace_test

import (
	"bytes"
	"context"
	"errors"
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
	ensured, id, err := trace.EnsureTraceIDWithReader(ctx, failingReader{})
	if err != nil {
		t.Fatalf("EnsureTraceIDWithReader() error = %v", err)
	}

	if id != "request-123" {
		t.Errorf("EnsureTraceID() id = %q, want request-123", id)
	}
	if got := trace.TraceID(ensured); got != "request-123" {
		t.Errorf("TraceID(ensured) = %q, want request-123", got)
	}
}

func TestEnsureTraceIDGeneratesSafeNonEmptyID(t *testing.T) {
	ctx, id, err := trace.EnsureTraceID(context.Background())
	if err != nil {
		t.Fatalf("EnsureTraceID() error = %v", err)
	}

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

func TestEnsureTraceIDWithReaderReturnsRandomSourceError(t *testing.T) {
	_, id, err := trace.EnsureTraceIDWithReader(context.Background(), failingReader{})
	if err == nil {
		t.Fatal("EnsureTraceIDWithReader() error = nil, want random source error")
	}
	if id != "" {
		t.Errorf("EnsureTraceIDWithReader() id = %q, want empty", id)
	}
}

func TestEnsureTraceIDWithReaderGeneratesID(t *testing.T) {
	ctx, id, err := trace.EnsureTraceIDWithReader(context.Background(), bytes.NewReader(make([]byte, 16)))
	if err != nil {
		t.Fatalf("EnsureTraceIDWithReader() error = %v", err)
	}
	if id == "" {
		t.Fatal("EnsureTraceIDWithReader() id is empty")
	}
	if got := trace.TraceID(ctx); got != id {
		t.Errorf("TraceID(ctx) = %q, want %q", got, id)
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) {
	return 0, errors.New("random source unavailable")
}
