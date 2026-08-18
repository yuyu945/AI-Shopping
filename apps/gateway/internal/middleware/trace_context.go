// Package middleware contains Gateway-wide HTTP middleware.
package middleware

import (
	"net/http"

	platformtrace "github.com/yuyu945/AI-Shopping/internal/platform/trace"
	"github.com/zeromicro/go-zero/rest/httpx"
)

const traceIDHeader = "X-Trace-ID"

// NewTraceContextMiddleware propagates a valid inbound trace ID to the local
// request context and Go-zero's OpenTelemetry context before route middleware.
func NewTraceContextMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		ctx, ok := platformtrace.WithRemoteTraceID(request.Context(), request.Header.Get(traceIDHeader))
		if !ok {
			next.ServeHTTP(writer, request)
			return
		}
		next.ServeHTTP(writer, request.WithContext(ctx))
	})
}

// NewTraceRouter wraps a Go-zero router so trace context is present before
// Go-zero's native trace and access-log middleware execute.
func NewTraceRouter(router httpx.Router) httpx.Router {
	return traceRouter{Router: router}
}

type traceRouter struct {
	httpx.Router
}

func (router traceRouter) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	NewTraceContextMiddleware(router.Router).ServeHTTP(writer, request)
}
