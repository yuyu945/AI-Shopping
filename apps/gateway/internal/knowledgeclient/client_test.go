package knowledgeclient

import (
	"context"
	"testing"
	"time"

	platformauth "github.com/yuyu945/AI-Shopping/internal/platform/auth"
	platformtrace "github.com/yuyu945/AI-Shopping/internal/platform/trace"
	knowledgepb "github.com/yuyu945/AI-Shopping/services/knowledge-service/gen"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

type recordingKnowledgeConn struct {
	ctx    context.Context
	method string
}

func (c *recordingKnowledgeConn) Invoke(ctx context.Context, method string, _ any, reply any, _ ...grpc.CallOption) error {
	c.ctx = ctx
	c.method = method
	switch out := reply.(type) {
	case *knowledgepb.UploadDocumentResponse:
		out.Document = &knowledgepb.Document{DocumentNo: "doc_1"}
	case *knowledgepb.SearchProductKnowledgeResponse:
		out.FallbackReason = "NO_SOURCE"
	}
	return nil
}

func (*recordingKnowledgeConn) NewStream(context.Context, *grpc.StreamDesc, string, ...grpc.CallOption) (grpc.ClientStream, error) {
	return nil, nil
}

func TestKnowledgeClientSearchForwardsBearerTraceAndDeadline(t *testing.T) {
	conn := &recordingKnowledgeConn{}
	ctx := platformtrace.WithTraceID(platformauth.ContextWithBearer(context.Background(), "Bearer token"), "4bf92f3577b34da6a3ce929d0e0e4736")

	out, err := NewGRPCClient(conn).SearchProductKnowledge(ctx, &knowledgepb.SearchProductKnowledgeRequest{ProductId: 1001, Query: "battery", TopK: 3})
	if err != nil {
		t.Fatal(err)
	}
	if out.GetFallbackReason() != "NO_SOURCE" {
		t.Fatalf("fallback_reason=%q", out.GetFallbackReason())
	}
	if conn.method != knowledgepb.KnowledgeService_SearchProductKnowledge_FullMethodName {
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
		t.Fatal("outgoing knowledge RPC context has no deadline")
	}
	if remaining := time.Until(deadline); remaining <= 0 || remaining > knowledgeCallTimeout {
		t.Fatalf("deadline remaining=%s, want (0,%s]", remaining, knowledgeCallTimeout)
	}
}

func TestKnowledgeClientSkipsInvalidTraceID(t *testing.T) {
	conn := &recordingKnowledgeConn{}
	ctx := platformtrace.WithTraceID(context.Background(), "not-a-trace")

	_, err := NewGRPCClient(conn).SearchProductKnowledge(ctx, &knowledgepb.SearchProductKnowledgeRequest{ProductId: 1001, Query: "battery"})
	if err != nil {
		t.Fatal(err)
	}
	md, _ := metadata.FromOutgoingContext(conn.ctx)
	if got := md.Get("trace_id"); len(got) != 0 {
		t.Fatalf("trace_id=%v", got)
	}
}
