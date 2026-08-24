package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequireOperatorHeaderRejectsMissingHeader(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/ops/agent-runs", nil)

	RequireOperatorHeader(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(w, r)

	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "FORBIDDEN") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestRequireOperatorHeaderAllowsTrueHeader(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/ops/agent-runs", nil)
	r.Header.Set("X-AI-Shopping-Operator", "true")

	RequireOperatorHeader(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(w, r)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}
