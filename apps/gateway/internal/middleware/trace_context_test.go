package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yuyu945/AI-Shopping/apps/gateway/internal/handler"
	platformtrace "github.com/yuyu945/AI-Shopping/internal/platform/trace"
	gozerotrace "github.com/zeromicro/go-zero/core/trace"
	resthandler "github.com/zeromicro/go-zero/rest/handler"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

const inboundTraceID = "4bf92f3577b34da6a3ce929d0e0e4736"
const conflictingTraceID = "11111111111111111111111111111111"

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

func TestTraceContextMiddlewareMakesXTraceIDAuthoritativeAcrossGatewayChain(t *testing.T) {
	previousProvider := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample())))
	t.Cleanup(func() { otel.SetTracerProvider(previousProvider) })

	previousPropagator := otel.GetTextMapPropagator()
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() { otel.SetTextMapPropagator(previousPropagator) })

	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	request.Header.Set("X-Trace-ID", inboundTraceID)
	request.Header.Set("traceparent", "00-"+conflictingTraceID+"-00f067aa0ba902b7-01")
	request.Header.Set("tracestate", "vendor=value")
	recorder := httptest.NewRecorder()

	nativeTrace := resthandler.TraceHandler("gateway", "/healthz")(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if got := platformtrace.TraceID(request.Context()); got != inboundTraceID {
			t.Errorf("platform trace ID = %q, want %q", got, inboundTraceID)
		}
		if got := gozerotrace.TraceIDFromContext(request.Context()); got != inboundTraceID {
			t.Errorf("Go-zero trace ID = %q, want %q", got, inboundTraceID)
		}
		handler.NewHealthHandler(nil).ServeHTTP(writer, request)
	}))

	NewTraceContextMiddleware(nativeTrace).ServeHTTP(recorder, request)

	if got := recorder.Header().Get("X-Trace-ID"); got != inboundTraceID {
		t.Errorf("response X-Trace-ID = %q, want %q", got, inboundTraceID)
	}
	if got := request.Header.Get("traceparent"); got != "00-"+inboundTraceID+"-0000000000000001-01" {
		t.Errorf("normalized traceparent = %q", got)
	}
	if got := request.Header.Get("tracestate"); got != "" {
		t.Errorf("tracestate = %q, want cleared conflicting state", got)
	}
}
