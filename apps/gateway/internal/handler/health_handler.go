// Package handler contains Gateway HTTP handlers.
package handler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/yuyu945/AI-Shopping/internal/platform/trace"
	gozerotrace "github.com/zeromicro/go-zero/core/trace"
)

const zeroTraceID = "00000000000000000000000000000000"

// TraceEnsurer attaches a trace ID to a request context when one is absent.
type TraceEnsurer func(context.Context) (context.Context, string, error)

// NewHealthHandler returns the Gateway readiness handler.
func NewHealthHandler(ensureTraceID TraceEnsurer) http.HandlerFunc {
	if ensureTraceID == nil {
		ensureTraceID = ensureW3CTraceID
	}

	return func(writer http.ResponseWriter, request *http.Request) {
		if traceID := gozerotrace.TraceIDFromContext(request.Context()); isValidTraceID(traceID) {
			writer.Header().Set("X-Trace-ID", traceID)
			writeJSON(writer, http.StatusOK, map[string]string{"status": "ok"})
			return
		}

		_, traceID, err := ensureTraceID(request.Context())
		if err != nil || !isValidTraceID(traceID) {
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

func ensureW3CTraceID(ctx context.Context) (context.Context, string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return ctx, "", fmt.Errorf("generate trace ID: %w", err)
	}
	traceID := hex.EncodeToString(bytes)
	return trace.WithTraceID(ctx, traceID), traceID, nil
}

func isValidTraceID(traceID string) bool {
	if len(traceID) != 32 || traceID == zeroTraceID {
		return false
	}
	for _, character := range traceID {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func writeJSON(writer http.ResponseWriter, status int, body map[string]string) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(body)
}
