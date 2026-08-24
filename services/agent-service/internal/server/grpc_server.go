package server

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	platformauth "github.com/yuyu945/AI-Shopping/internal/platform/auth"
	agentpb "github.com/yuyu945/AI-Shopping/services/agent-service/gen"
	"github.com/yuyu945/AI-Shopping/services/agent-service/internal/agent"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// RunStarter is the bounded Agent run use case exposed over RPC.
type RunStarter interface {
	StartRun(context.Context, agent.StartRunCommand) (agent.RunOutcome, error)
}

// RunLoader loads an owned run timeline.
type RunLoader interface {
	GetRunTimeline(context.Context, uint64, string) (agent.RunTimeline, error)
}

// GRPCServer exposes Agent run APIs over the generated contract.
type GRPCServer struct {
	agentpb.UnimplementedAgentServiceServer
	starter RunStarter
	loader  RunLoader
	auth    *platformauth.Manager
	timeout time.Duration
}

// NewGRPCServer constructs an Agent gRPC server.
func NewGRPCServer(starter RunStarter, loader RunLoader, auth *platformauth.Manager, timeout time.Duration) *GRPCServer {
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	return &GRPCServer{starter: starter, loader: loader, auth: auth, timeout: timeout}
}

func (s *GRPCServer) StartRun(ctx context.Context, req *agentpb.StartRunRequest) (*agentpb.StartRunResponse, error) {
	userID, err := s.userID(ctx)
	if err != nil {
		return nil, err
	}
	if req == nil || strings.TrimSpace(req.GetUserInput()) == "" {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	if s.starter == nil {
		return nil, status.Error(codes.Internal, "internal server error")
	}
	sessionNo := strings.TrimSpace(req.GetSessionNo())
	if sessionNo == "" {
		sessionNo = "sess_" + uuid.NewString()
	}
	callCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	outcome, err := s.starter.StartRun(callCtx, agent.StartRunCommand{
		SessionNo: sessionNo, RunID: "run_" + uuid.NewString(), UserID: userID, TraceID: uuid.NewString(),
		UserInput: strings.TrimSpace(req.GetUserInput()), ModelName: "disabled", PromptVersion: "m4.1-v1", Now: time.Now(),
	})
	if err != nil {
		if errors.Is(callCtx.Err(), context.DeadlineExceeded) {
			return nil, status.Error(codes.DeadlineExceeded, "dependency timeout")
		}
		return nil, toStatus(err)
	}
	return &agentpb.StartRunResponse{Run: &agentpb.AgentRun{
		RunId: outcome.RunID, SessionNo: sessionNo, UserId: userID, Status: string(outcome.Status),
		FinalText: outcome.FinalText, ErrorCode: outcome.ErrorCode,
	}}, nil
}

func (s *GRPCServer) GetRun(ctx context.Context, req *agentpb.GetRunRequest) (*agentpb.GetRunResponse, error) {
	userID, err := s.userID(ctx)
	if err != nil {
		return nil, err
	}
	if req == nil || strings.TrimSpace(req.GetRunId()) == "" {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	if s.loader == nil {
		return nil, status.Error(codes.Internal, "internal server error")
	}
	callCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	timeline, err := s.loader.GetRunTimeline(callCtx, userID, strings.TrimSpace(req.GetRunId()))
	if err != nil {
		if errors.Is(callCtx.Err(), context.DeadlineExceeded) {
			return nil, status.Error(codes.DeadlineExceeded, "dependency timeout")
		}
		return nil, toStatus(err)
	}
	return &agentpb.GetRunResponse{Run: runWire(timeline.Run), Steps: stepWire(timeline.Steps), Recommendations: recommendationWire(timeline.Recommendations)}, nil
}

func (s *GRPCServer) userID(ctx context.Context) (uint64, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok || len(md.Get("authorization")) != 1 || s.auth == nil {
		return 0, status.Error(codes.Unauthenticated, "authentication required")
	}
	principal, err := s.auth.VerifyBearer(md.Get("authorization")[0], time.Now())
	if err != nil {
		return 0, status.Error(codes.Unauthenticated, "authentication required")
	}
	return principal.UserID, nil
}

func runWire(run agent.Run) *agentpb.AgentRun {
	return &agentpb.AgentRun{
		RunId: run.RunID, UserId: run.UserID, Status: string(run.Status), ErrorCode: run.ErrorCode,
		ErrorMessage: run.ErrorMessage, StepCount: run.StepCount,
	}
}

func stepWire(steps []agent.Step) []*agentpb.AgentStep {
	out := make([]*agentpb.AgentStep, 0, len(steps))
	for _, step := range steps {
		out = append(out, &agentpb.AgentStep{
			StepNo: step.StepNo, StepType: string(step.StepType), ToolName: step.ToolName, Status: string(step.Status),
			ErrorCode: step.ErrorCode, ErrorMessage: step.ErrorMessage, LatencyMs: step.LatencyMS,
		})
	}
	return out
}

func recommendationWire(items []agent.RecommendationSnapshot) []*agentpb.Recommendation {
	out := make([]*agentpb.Recommendation, 0, len(items))
	for _, item := range items {
		out = append(out, &agentpb.Recommendation{
			RankNo:           item.RankNo,
			SkuId:            item.SKUID,
			ProductId:        item.ProductID,
			ProductTitle:     item.ProductTitleSnapshot,
			SkuCode:          item.SKUCodeSnapshot,
			SkuSpecJson:      append([]byte(nil), item.SKUSpecSnapshotJSON...),
			Price:            item.PriceSnapshot,
			Saleable:         item.SaleableSnapshot,
			DiscountJson:     append([]byte(nil), item.DiscountSnapshotJSON...),
			Reason:           item.Reason,
			ValidationStatus: string(item.ValidationStatus),
		})
	}
	return out
}

func toStatus(err error) error {
	switch {
	case errors.Is(err, agent.ErrRunNotFound):
		return status.Error(codes.NotFound, "resource not found")
	case errors.Is(err, agent.ErrInvalidToolArgument):
		return status.Error(codes.InvalidArgument, "invalid request")
	case errors.Is(err, agent.ErrDependencyTimeout), errors.Is(err, context.DeadlineExceeded):
		return status.Error(codes.DeadlineExceeded, "dependency timeout")
	case errors.Is(err, agent.ErrUnknownTool), errors.Is(err, agent.ErrMaxStepsExceeded), errors.Is(err, agent.ErrModelFailed), errors.Is(err, agent.ErrToolFailed), errors.Is(err, agent.ErrInvalidFinalRecommendation), errors.Is(err, agent.ErrNoValidRecommendation):
		return status.Error(codes.FailedPrecondition, "agent run failed")
	default:
		return status.Error(codes.Internal, "internal server error")
	}
}
