package orderclient

import (
	"context"
	platformauth "github.com/yuyu945/AI-Shopping/internal/platform/auth"
	platformtrace "github.com/yuyu945/AI-Shopping/internal/platform/trace"
	orderpb "github.com/yuyu945/AI-Shopping/services/order-service/gen"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"testing"
	"time"
)

type recordingConn struct {
	ctx    context.Context
	method string
}

func (c *recordingConn) Invoke(ctx context.Context, method string, _ any, reply any, _ ...grpc.CallOption) error {
	c.ctx = ctx
	c.method = method
	switch value := reply.(type) {
	case *orderpb.GetCartResponse:
		value.Cart = &orderpb.Cart{}
	case *orderpb.ReviewResponse:
		value.Review = &orderpb.Review{ReviewNo: "REV-1"}
	}
	return nil
}
func (*recordingConn) NewStream(context.Context, *grpc.StreamDesc, string, ...grpc.CallOption) (grpc.ClientStream, error) {
	return nil, nil
}
func TestClientForwardsVerifiedBearer(t *testing.T) {
	conn := &recordingConn{}
	ctx := platformtrace.WithTraceID(platformauth.ContextWithBearer(context.Background(), "Bearer token"), "4bf92f3577b34da6a3ce929d0e0e4736")
	_, e := NewGRPCClient(conn).GetCart(ctx, &orderpb.GetCartRequest{})
	if e != nil {
		t.Fatal(e)
	}
	md, _ := metadata.FromOutgoingContext(conn.ctx)
	if got := md.Get("authorization"); len(got) != 1 || got[0] != "Bearer token" {
		t.Fatalf("authorization=%v", got)
	}
	if got := md.Get("trace_id"); len(got) != 1 || got[0] != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("trace_id=%v", got)
	}
}

func TestClientAddsDeadlineBeforeForwardingMetadata(t *testing.T) {
	conn := &recordingConn{}
	_, err := NewGRPCClient(conn).GetCart(context.Background(), &orderpb.GetCartRequest{})
	if err != nil {
		t.Fatal(err)
	}
	deadline, ok := conn.ctx.Deadline()
	if !ok {
		t.Fatal("outgoing order RPC context has no deadline")
	}
	if remaining := time.Until(deadline); remaining <= 0 || remaining > orderCallTimeout {
		t.Fatalf("outgoing deadline remaining = %s, want (0,%s]", remaining, orderCallTimeout)
	}
}

func TestClientSubmitReviewForwardsMethodBearerTraceAndDeadline(t *testing.T) {
	conn := &recordingConn{}
	ctx := platformtrace.WithTraceID(platformauth.ContextWithBearer(context.Background(), "Bearer token"), "4bf92f3577b34da6a3ce929d0e0e4736")

	_, err := NewGRPCClient(conn).SubmitReview(ctx, &orderpb.SubmitReviewRequest{OrderNo: "ORD-1", SkuId: 101, Rating: 5, Content: "good"})
	if err != nil {
		t.Fatal(err)
	}
	if conn.method != orderpb.OrderService_SubmitReview_FullMethodName {
		t.Fatalf("method = %q, want %q", conn.method, orderpb.OrderService_SubmitReview_FullMethodName)
	}
	md, _ := metadata.FromOutgoingContext(conn.ctx)
	if got := md.Get("authorization"); len(got) != 1 || got[0] != "Bearer token" {
		t.Fatalf("authorization=%v", got)
	}
	if got := md.Get("trace_id"); len(got) != 1 || got[0] != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("trace_id=%v", got)
	}
	if _, ok := conn.ctx.Deadline(); !ok {
		t.Fatal("outgoing SubmitReview context has no deadline")
	}
}
