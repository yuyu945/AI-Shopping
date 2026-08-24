package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	agentpb "github.com/yuyu945/AI-Shopping/services/agent-service/gen"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakeAgentClient struct {
	start func(context.Context, *agentpb.StartRunRequest) (*agentpb.StartRunResponse, error)
	get   func(context.Context, *agentpb.GetRunRequest) (*agentpb.GetRunResponse, error)
}

func (f *fakeAgentClient) StartRun(ctx context.Context, req *agentpb.StartRunRequest) (*agentpb.StartRunResponse, error) {
	if f.start != nil {
		return f.start(ctx, req)
	}
	return &agentpb.StartRunResponse{Run: &agentpb.AgentRun{RunId: "run_1", SessionNo: "sess_1", Status: "FAILED", ErrorCode: "MODEL_FAILED", StepCount: 1}}, nil
}

func (f *fakeAgentClient) GetRun(ctx context.Context, req *agentpb.GetRunRequest) (*agentpb.GetRunResponse, error) {
	if f.get != nil {
		return f.get(ctx, req)
	}
	return &agentpb.GetRunResponse{Run: &agentpb.AgentRun{RunId: req.GetRunId(), Status: "SUCCEEDED"}}, nil
}

func TestAgentHandlerCreateRunMapsRequestAndResponse(t *testing.T) {
	client := &fakeAgentClient{start: func(_ context.Context, req *agentpb.StartRunRequest) (*agentpb.StartRunResponse, error) {
		if req.GetSessionNo() != "sess_1" || req.GetUserInput() != "buy laptop" {
			t.Fatalf("req=%#v", req)
		}
		return &agentpb.StartRunResponse{Run: &agentpb.AgentRun{RunId: "run_1", SessionNo: "sess_1", Status: "FAILED", ErrorCode: "MODEL_FAILED", StepCount: 1}}, nil
	}}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/agent/runs", strings.NewReader(`{"session_no":" sess_1 ","message":" buy laptop "}`))

	NewAgentHandler(client).Runs().ServeHTTP(w, r)

	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"run_id":"run_1"`) || !strings.Contains(w.Body.String(), `"stream_url":"/api/v1/agent/runs/run_1/events"`) {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestAgentHandlerRejectsEmptyCreateMessage(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/agent/runs", strings.NewReader(`{"message":"   "}`))

	NewAgentHandler(&fakeAgentClient{}).Runs().ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "INVALID_ARGUMENT") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestAgentHandlerGetRunMapsTimelineAndValidJSONSnapshots(t *testing.T) {
	client := &fakeAgentClient{get: func(_ context.Context, req *agentpb.GetRunRequest) (*agentpb.GetRunResponse, error) {
		if req.GetRunId() != "run_1" {
			t.Fatalf("run_id=%q", req.GetRunId())
		}
		return &agentpb.GetRunResponse{
			Run:   &agentpb.AgentRun{RunId: "run_1", SessionNo: "sess_1", Status: "SUCCEEDED", FinalText: "done", StepCount: 1},
			Steps: []*agentpb.AgentStep{{StepNo: 1, StepType: "TOOL", ToolName: "search_products", Status: "SUCCEEDED", LatencyMs: 45}},
			Recommendations: []*agentpb.Recommendation{{
				RankNo: 1, SkuId: 2001, ProductId: 1001, ProductTitle: "ThinkPad", SkuCode: "TP",
				SkuSpecJson: nil, Price: "4999.00", Saleable: true, DiscountJson: nil,
				Reason: "fits", ValidationStatus: "VERIFIED",
			}},
		}, nil
	}}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/agent/runs/run_1", nil)
	r.SetPathValue("run_id", "run_1")

	NewAgentHandler(client).Run().ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"sku_spec_json":{}`) || !strings.Contains(w.Body.String(), `"discount_json":[]`) {
		t.Fatalf("invalid json defaults: %s", w.Body.String())
	}
	var decoded map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("response is not valid json: %v body=%s", err, w.Body.String())
	}
}

func TestAgentHandlerMapsStableErrorsWithoutRawDetails(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
		code string
	}{
		{"invalid", status.Error(codes.InvalidArgument, "raw validation"), http.StatusBadRequest, "INVALID_ARGUMENT"},
		{"unauthenticated", status.Error(codes.Unauthenticated, "jwt body"), http.StatusUnauthorized, "UNAUTHENTICATED"},
		{"not found", status.Error(codes.NotFound, "sql details"), http.StatusNotFound, "NOT_FOUND"},
		{"failed precondition", status.Error(codes.FailedPrecondition, "model body"), http.StatusConflict, "AGENT_RUN_FAILED"},
		{"timeout", status.Error(codes.DeadlineExceeded, "dial secret"), http.StatusGatewayTimeout, "DEPENDENCY_TIMEOUT"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := NewAgentHandler(&fakeAgentClient{get: func(context.Context, *agentpb.GetRunRequest) (*agentpb.GetRunResponse, error) {
				return nil, tc.err
			}})
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodGet, "/api/v1/agent/runs/run_1", nil)
			r.SetPathValue("run_id", "run_1")
			h.Run().ServeHTTP(w, r)
			if w.Code != tc.want || !strings.Contains(w.Body.String(), tc.code) || strings.Contains(w.Body.String(), "raw") || strings.Contains(w.Body.String(), "secret") || strings.Contains(w.Body.String(), "sql") || strings.Contains(w.Body.String(), "model") {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func TestAgentHandlerSSEEmitsReplayEvents(t *testing.T) {
	client := &fakeAgentClient{get: func(_ context.Context, req *agentpb.GetRunRequest) (*agentpb.GetRunResponse, error) {
		return &agentpb.GetRunResponse{
			Run:             &agentpb.AgentRun{RunId: req.GetRunId(), Status: "SUCCEEDED"},
			Steps:           []*agentpb.AgentStep{{StepNo: 1, ToolName: "search_products", Status: "SUCCEEDED"}},
			Recommendations: []*agentpb.Recommendation{{RankNo: 1, SkuId: 2001, ValidationStatus: "VERIFIED"}},
		}, nil
	}}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/agent/runs/run_1/events", nil)
	r.SetPathValue("run_id", "run_1")

	NewAgentHandler(client).Events().ServeHTTP(w, r)

	body := w.Body.String()
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, body)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("content-type=%q", ct)
	}
	for _, want := range []string{"event: run_snapshot", "event: step_snapshot", "event: recommendation_snapshot", "event: run_completed"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in %s", want, body)
		}
	}
}
