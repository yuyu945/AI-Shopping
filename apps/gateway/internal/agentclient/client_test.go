package agentclient

import (
	"context"
	"testing"
	"time"

	platformauth "github.com/yuyu945/AI-Shopping/internal/platform/auth"
	platformtrace "github.com/yuyu945/AI-Shopping/internal/platform/trace"
	agentpb "github.com/yuyu945/AI-Shopping/services/agent-service/gen"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

type recordingAgentConn struct {
	ctx    context.Context
	method string
}

func (c *recordingAgentConn) Invoke(ctx context.Context, method string, _ any, reply any, _ ...grpc.CallOption) error {
	c.ctx = ctx
	c.method = method
	switch out := reply.(type) {
	case *agentpb.StartRunResponse:
		out.Run = &agentpb.AgentRun{RunId: "run_1", SessionNo: "sess_1", Status: "FAILED"}
	case *agentpb.GetRunResponse:
		out.Run = &agentpb.AgentRun{RunId: "run_1", Status: "SUCCEEDED"}
	}
	return nil
}

func (*recordingAgentConn) NewStream(context.Context, *grpc.StreamDesc, string, ...grpc.CallOption) (grpc.ClientStream, error) {
	return nil, nil
}

func TestAgentClientForwardsBearerTraceAndDeadline(t *testing.T) {
	conn := &recordingAgentConn{}
	ctx := platformtrace.WithTraceID(platformauth.ContextWithBearer(context.Background(), "Bearer token"), "4bf92f3577b34da6a3ce929d0e0e4736")

	out, err := NewGRPCClient(conn).StartRun(ctx, &agentpb.StartRunRequest{UserInput: "buy laptop"})
	if err != nil {
		t.Fatal(err)
	}
	if out.GetRun().GetRunId() != "run_1" {
		t.Fatalf("run_id=%q", out.GetRun().GetRunId())
	}
	if conn.method != agentpb.AgentService_StartRun_FullMethodName {
		t.Fatalf("method=%q", conn.method)
	}
	md, _ := metadata.FromOutgoingContext(conn.ctx)
	if got := md.Get("authorization"); len(got) != 1 || got[0] != "Bearer token" {
		t.Fatalf("authorization=%v", got)
	}
	if got := md.Get("trace_id"); len(got) != 1 || got[0] != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("trace_id=%v", got)
	}
	deadline, ok := conn.ctx.Deadline()
	if !ok {
		t.Fatal("outgoing agent RPC context has no deadline")
	}
	if remaining := time.Until(deadline); remaining <= 0 || remaining > agentCallTimeout {
		t.Fatalf("deadline remaining=%s, want (0,%s]", remaining, agentCallTimeout)
	}
}

func TestAgentClientSkipsInvalidTraceID(t *testing.T) {
	conn := &recordingAgentConn{}
	ctx := platformtrace.WithTraceID(context.Background(), "not-a-trace")

	_, err := NewGRPCClient(conn).GetRun(ctx, &agentpb.GetRunRequest{RunId: "run_1"})
	if err != nil {
		t.Fatal(err)
	}
	if conn.method != agentpb.AgentService_GetRun_FullMethodName {
		t.Fatalf("method=%q", conn.method)
	}
	md, _ := metadata.FromOutgoingContext(conn.ctx)
	if got := md.Get("trace_id"); len(got) != 0 {
		t.Fatalf("trace_id=%v", got)
	}
}
