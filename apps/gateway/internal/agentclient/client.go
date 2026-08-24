package agentclient

import (
	"context"
	"time"

	platformauth "github.com/yuyu945/AI-Shopping/internal/platform/auth"
	platformtrace "github.com/yuyu945/AI-Shopping/internal/platform/trace"
	agentpb "github.com/yuyu945/AI-Shopping/services/agent-service/gen"
	oteltrace "go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

const agentCallTimeout = 2 * time.Second

type Client interface {
	StartRun(context.Context, *agentpb.StartRunRequest) (*agentpb.StartRunResponse, error)
	GetRun(context.Context, *agentpb.GetRunRequest) (*agentpb.GetRunResponse, error)
	ListRuns(context.Context, *agentpb.ListRunsRequest) (*agentpb.ListRunsResponse, error)
	GetRunOps(context.Context, *agentpb.GetRunOpsRequest) (*agentpb.GetRunOpsResponse, error)
}

type grpcClient struct {
	client agentpb.AgentServiceClient
}

func NewGRPCClient(conn grpc.ClientConnInterface) Client {
	return &grpcClient{client: agentpb.NewAgentServiceClient(conn)}
}

func (c *grpcClient) StartRun(parent context.Context, req *agentpb.StartRunRequest) (*agentpb.StartRunResponse, error) {
	ctx, cancel := c.auth(parent)
	defer cancel()
	return c.client.StartRun(ctx, req)
}

func (c *grpcClient) GetRun(parent context.Context, req *agentpb.GetRunRequest) (*agentpb.GetRunResponse, error) {
	ctx, cancel := c.auth(parent)
	defer cancel()
	return c.client.GetRun(ctx, req)
}

func (c *grpcClient) ListRuns(parent context.Context, req *agentpb.ListRunsRequest) (*agentpb.ListRunsResponse, error) {
	ctx, cancel := c.auth(parent)
	defer cancel()
	return c.client.ListRuns(ctx, req)
}

func (c *grpcClient) GetRunOps(parent context.Context, req *agentpb.GetRunOpsRequest) (*agentpb.GetRunOpsResponse, error) {
	ctx, cancel := c.auth(parent)
	defer cancel()
	return c.client.GetRunOps(ctx, req)
}

func (c *grpcClient) auth(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(parent, agentCallTimeout)
	if bearer, ok := platformauth.BearerFromContext(ctx); ok {
		ctx = metadata.AppendToOutgoingContext(ctx, "authorization", bearer)
	}
	if traceID := platformtrace.TraceID(ctx); validTraceID(traceID) {
		ctx = metadata.AppendToOutgoingContext(ctx, "trace_id", traceID)
	}
	return ctx, cancel
}

func validTraceID(value string) bool {
	_, err := oteltrace.TraceIDFromHex(value)
	return err == nil
}
