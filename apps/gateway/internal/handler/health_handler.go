// Package handler contains Gateway HTTP handlers.
package handler

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/yuyu945/AI-Shopping/internal/platform/trace"
	gozerotrace "github.com/zeromicro/go-zero/core/trace"
)

// TraceEnsurer attaches a trace ID to a request context when one is absent.
type TraceEnsurer func(context.Context) (context.Context, string, error)

// NewHealthHandler returns the Gateway readiness handler.
func NewHealthHandler(ensureTraceID TraceEnsurer) http.HandlerFunc {
	if ensureTraceID == nil {
		ensureTraceID = trace.EnsureTraceID
	}

	return func(writer http.ResponseWriter, request *http.Request) {
		if traceID := gozerotrace.TraceIDFromContext(request.Context()); traceID != "" {
			writer.Header().Set("X-Trace-ID", traceID)
			writeJSON(writer, http.StatusOK, map[string]string{"status": "ok"})
			return
		}

		ctx := request.Context()
		if traceID := request.Header.Get("X-Trace-ID"); traceID != "" {
			ctx = trace.WithTraceID(ctx, traceID)
		}

		_, traceID, err := ensureTraceID(ctx)
		if err != nil {
			writeJSON(writer, http.StatusInternalServerError, map[string]string{
				"code":    "INTERNAL",
				"message": "internal server error",
			})
			return
		}

		writer.Header().Set("X-Trace-ID", traceID)
		writeJSON(writer, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func writeJSON(writer http.ResponseWriter, status int, body map[string]string) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(body)
}
