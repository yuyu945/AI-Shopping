package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/yuyu945/AI-Shopping/internal/platform/trace"
)

func TestHealthHandlerReturnsProvidedTraceID(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	request.Header.Set("X-Trace-ID", "request-123")
	recorder := httptest.NewRecorder()

	NewHealthHandler(trace.EnsureTraceID).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if got := recorder.Header().Get("X-Trace-ID"); got != "request-123" {
		t.Errorf("X-Trace-ID = %q, want request-123", got)
	}
	assertJSONBody(t, recorder, map[string]string{"status": "ok"})
}

func TestHealthHandlerGeneratesTraceID(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	recorder := httptest.NewRecorder()

	NewHealthHandler(trace.EnsureTraceID).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	traceID := recorder.Header().Get("X-Trace-ID")
	if !regexp.MustCompile(`^[A-Za-z0-9_-]+$`).MatchString(traceID) {
		t.Errorf("generated X-Trace-ID = %q, want URL-safe non-empty value", traceID)
	}
	assertJSONBody(t, recorder, map[string]string{"status": "ok"})
}

func TestHealthHandlerReturnsSafeErrorWhenTraceGenerationFails(t *testing.T) {
	traceFailure := errors.New("random source unavailable: secret=must-not-leak")
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	recorder := httptest.NewRecorder()

	NewHealthHandler(func(ctx context.Context) (context.Context, string, error) {
		return ctx, "", traceFailure
	}).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	if recorder.Header().Get("X-Trace-ID") != "" {
		t.Errorf("X-Trace-ID = %q, want empty on trace failure", recorder.Header().Get("X-Trace-ID"))
	}
	assertJSONBody(t, recorder, map[string]string{
		"code":    "INTERNAL",
		"message": "internal server error",
	})
	if strings.Contains(recorder.Body.String(), "must-not-leak") {
		t.Fatalf("error response leaked trace failure: %s", recorder.Body.String())
	}
}

func assertJSONBody(t *testing.T, recorder *httptest.ResponseRecorder, want map[string]string) {
	t.Helper()
	if contentType := recorder.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", contentType)
	}

	var got map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
		t.Fatalf("response body is not JSON: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("JSON field count = %d, want %d; body=%s", len(got), len(want), recorder.Body.String())
	}
	for key, value := range want {
		if got[key] != value {
			t.Errorf("JSON[%q] = %q, want %q", key, got[key], value)
		}
	}
}
