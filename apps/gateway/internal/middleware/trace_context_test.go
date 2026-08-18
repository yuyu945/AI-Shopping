package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	platformtrace "github.com/yuyu945/AI-Shopping/internal/platform/trace"
	gozerotrace "github.com/zeromicro/go-zero/core/trace"
)

const inboundTraceID = "4bf92f3577b34da6a3ce929d0e0e4736"

func TestTraceContextMiddlewarePropagatesInboundTraceIDToRequestContext(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	request.Header.Set("X-Trace-ID", inboundTraceID)
	recorder := httptest.NewRecorder()

	NewTraceContextMiddleware(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if got := platformtrace.TraceID(request.Context()); got != inboundTraceID {
			t.Errorf("platform trace ID = %q, want %q", got, inboundTraceID)
		}
		if got := gozerotrace.TraceIDFromContext(request.Context()); got != inboundTraceID {
			t.Errorf("Go-zero trace ID = %q, want %q", got, inboundTraceID)
		}
		writer.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusNoContent)
	}
}
