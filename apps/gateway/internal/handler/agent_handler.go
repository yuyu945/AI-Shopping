package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/yuyu945/AI-Shopping/apps/gateway/internal/agentclient"
	agentpb "github.com/yuyu945/AI-Shopping/services/agent-service/gen"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type AgentHandler struct {
	client agentclient.Client
}

func NewAgentHandler(client agentclient.Client) *AgentHandler {
	return &AgentHandler{client: client}
}

func (h *AgentHandler) Runs() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeAgentError(w, status.Error(codes.InvalidArgument, "invalid request"))
			return
		}
		var body struct {
			SessionNo string `json:"session_no"`
			Message   string `json:"message"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeAgentError(w, status.Error(codes.InvalidArgument, "invalid request"))
			return
		}
		message := strings.TrimSpace(body.Message)
		if message == "" {
			writeAgentError(w, status.Error(codes.InvalidArgument, "invalid request"))
			return
		}
		out, err := h.client.StartRun(r.Context(), &agentpb.StartRunRequest{
			SessionNo: strings.TrimSpace(body.SessionNo),
			UserInput: message,
		})
		if err != nil {
			writeAgentError(w, err)
			return
		}
		writeJSONValue(w, http.StatusOK, map[string]any{"run": agentRunJSON(out.GetRun())})
	}
}

func (h *AgentHandler) Run() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		runID := strings.TrimSpace(r.PathValue("run_id"))
		if runID == "" {
			writeAgentError(w, status.Error(codes.InvalidArgument, "invalid request"))
			return
		}
		out, err := h.client.GetRun(r.Context(), &agentpb.GetRunRequest{RunId: runID})
		if err != nil {
			writeAgentError(w, err)
			return
		}
		writeJSONValue(w, http.StatusOK, agentTimelineJSON(out))
	}
}

func (h *AgentHandler) OpsRuns() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pageSize, _ := strconv.ParseUint(r.URL.Query().Get("page_size"), 10, 32)
		userID, _ := strconv.ParseUint(r.URL.Query().Get("user_id"), 10, 64)
		out, err := h.client.ListRuns(r.Context(), &agentpb.ListRunsRequest{
			Status:    strings.TrimSpace(r.URL.Query().Get("status")),
			UserId:    userID,
			PageSize:  uint32(pageSize),
			PageToken: strings.TrimSpace(r.URL.Query().Get("page_token")),
		})
		if err != nil {
			writeAgentError(w, err)
			return
		}
		runs := make([]any, 0, len(out.GetRuns()))
		for _, run := range out.GetRuns() {
			runs = append(runs, agentRunJSON(run))
		}
		writeJSONValue(w, http.StatusOK, map[string]any{"runs": runs, "next_page_token": out.GetNextPageToken()})
	}
}

func (h *AgentHandler) OpsRun() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		runID := strings.TrimSpace(r.PathValue("run_id"))
		if runID == "" {
			writeAgentError(w, status.Error(codes.InvalidArgument, "invalid request"))
			return
		}
		out, err := h.client.GetRunOps(r.Context(), &agentpb.GetRunOpsRequest{RunId: runID})
		if err != nil {
			writeAgentError(w, err)
			return
		}
		steps := make([]any, 0, len(out.GetSteps()))
		for _, step := range out.GetSteps() {
			steps = append(steps, agentStepOpsJSON(step))
		}
		recommendations := make([]any, 0, len(out.GetRecommendations()))
		for _, recommendation := range out.GetRecommendations() {
			recommendations = append(recommendations, recommendationJSON(recommendation))
		}
		writeJSONValue(w, http.StatusOK, map[string]any{"run": agentRunJSON(out.GetRun()), "steps": steps, "recommendations": recommendations})
	}
}

func (h *AgentHandler) Events() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		runID := strings.TrimSpace(r.PathValue("run_id"))
		if runID == "" {
			writeAgentError(w, status.Error(codes.InvalidArgument, "invalid request"))
			return
		}
		out, err := h.client.GetRun(r.Context(), &agentpb.GetRunRequest{RunId: runID})
		if err != nil {
			writeAgentError(w, err)
			return
		}
		header := w.Header()
		header.Set("Content-Type", "text/event-stream")
		header.Set("Cache-Control", "no-cache")
		header.Set("Connection", "keep-alive")
		writeSSE(w, "run_snapshot", map[string]any{"run": agentRunJSON(out.GetRun())})
		for _, step := range out.GetSteps() {
			writeSSE(w, "step_snapshot", map[string]any{"step": agentStepJSON(step)})
		}
		for _, recommendation := range out.GetRecommendations() {
			writeSSE(w, "recommendation_snapshot", map[string]any{"recommendation": recommendationJSON(recommendation)})
		}
		switch out.GetRun().GetStatus() {
		case "SUCCEEDED":
			writeSSE(w, "run_completed", map[string]any{"run_id": out.GetRun().GetRunId(), "status": out.GetRun().GetStatus()})
		case "FAILED", "TIMEOUT":
			writeSSE(w, "run_failed", map[string]any{
				"run_id": out.GetRun().GetRunId(),
				"status": out.GetRun().GetStatus(),
				"code":   out.GetRun().GetErrorCode(),
			})
		default:
			writeSSE(w, "heartbeat", map[string]any{
				"run_id": runID,
				"status": out.GetRun().GetStatus(),
				"at":     time.Now().UTC().Format(time.RFC3339),
			})
		}
	}
}

func agentTimelineJSON(out *agentpb.GetRunResponse) map[string]any {
	steps := make([]any, 0, len(out.GetSteps()))
	for _, step := range out.GetSteps() {
		steps = append(steps, agentStepJSON(step))
	}
	recommendations := make([]any, 0, len(out.GetRecommendations()))
	for _, recommendation := range out.GetRecommendations() {
		recommendations = append(recommendations, recommendationJSON(recommendation))
	}
	return map[string]any{"run": agentRunJSON(out.GetRun()), "steps": steps, "recommendations": recommendations}
}

func agentRunJSON(run *agentpb.AgentRun) map[string]any {
	if run == nil {
		return nil
	}
	return map[string]any{
		"run_id":         run.GetRunId(),
		"session_no":     run.GetSessionNo(),
		"status":         run.GetStatus(),
		"final_text":     run.GetFinalText(),
		"error_code":     run.GetErrorCode(),
		"step_count":     run.GetStepCount(),
		"stream_url":     "/api/v1/agent/runs/" + run.GetRunId() + "/events",
		"trace_id":       run.GetTraceId(),
		"model_name":     run.GetModelName(),
		"prompt_version": run.GetPromptVersion(),
		"started_at":     run.GetStartedAt(),
		"ended_at":       run.GetEndedAt(),
		"created_at":     run.GetCreatedAt(),
	}
}

func agentStepJSON(step *agentpb.AgentStep) map[string]any {
	if step == nil {
		return nil
	}
	return map[string]any{
		"step_no":    step.GetStepNo(),
		"step_type":  step.GetStepType(),
		"tool_name":  step.GetToolName(),
		"status":     step.GetStatus(),
		"error_code": step.GetErrorCode(),
		"latency_ms": step.GetLatencyMs(),
	}
}

func agentStepOpsJSON(step *agentpb.AgentStep) map[string]any {
	base := agentStepJSON(step)
	if base == nil {
		return nil
	}
	base["attempt"] = step.GetAttempt()
	base["input_json"] = rawJSONOrDefault(step.GetInputJson(), "{}")
	base["output_json"] = rawJSONOrDefault(step.GetOutputJson(), "{}")
	base["started_at"] = step.GetStartedAt()
	base["ended_at"] = step.GetEndedAt()
	return base
}

func recommendationJSON(value *agentpb.Recommendation) map[string]any {
	if value == nil {
		return nil
	}
	return map[string]any{
		"rank_no":           value.GetRankNo(),
		"sku_id":            value.GetSkuId(),
		"product_id":        value.GetProductId(),
		"product_title":     value.GetProductTitle(),
		"sku_code":          value.GetSkuCode(),
		"sku_spec_json":     rawJSONOrDefault(value.GetSkuSpecJson(), "{}"),
		"price":             value.GetPrice(),
		"saleable":          value.GetSaleable(),
		"discount_json":     rawJSONOrDefault(value.GetDiscountJson(), "[]"),
		"reason":            value.GetReason(),
		"validation_status": value.GetValidationStatus(),
	}
}

func rawJSONOrDefault(value []byte, fallback string) json.RawMessage {
	if len(value) == 0 || !json.Valid(value) {
		return json.RawMessage(fallback)
	}
	return json.RawMessage(value)
}

func writeSSE(w http.ResponseWriter, event string, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		data = []byte(`{"code":"INTERNAL","message":"internal server error"}`)
	}
	_, _ = w.Write([]byte("event: " + event + "\n"))
	_, _ = w.Write([]byte("data: "))
	_, _ = w.Write(data)
	_, _ = w.Write([]byte("\n\n"))
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

func writeAgentError(w http.ResponseWriter, err error) {
	code := codes.Internal
	if statusErr, ok := status.FromError(err); ok {
		code = statusErr.Code()
	}
	httpCode := http.StatusInternalServerError
	body := map[string]string{"code": "INTERNAL", "message": "internal server error"}
	switch code {
	case codes.InvalidArgument:
		httpCode = http.StatusBadRequest
		body = map[string]string{"code": "INVALID_ARGUMENT", "message": "invalid request"}
	case codes.Unauthenticated:
		httpCode = http.StatusUnauthorized
		body = map[string]string{"code": "UNAUTHENTICATED", "message": "authentication required"}
	case codes.NotFound:
		httpCode = http.StatusNotFound
		body = map[string]string{"code": "NOT_FOUND", "message": "resource not found"}
	case codes.FailedPrecondition:
		httpCode = http.StatusConflict
		body = map[string]string{"code": "AGENT_RUN_FAILED", "message": "agent run failed"}
	case codes.DeadlineExceeded, codes.Unavailable:
		httpCode = http.StatusGatewayTimeout
		body = map[string]string{"code": "DEPENDENCY_TIMEOUT", "message": "agent service timeout"}
	}
	writeJSONValue(w, httpCode, body)
}
