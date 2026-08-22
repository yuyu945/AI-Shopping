package orderclient

import (
	"context"
	platformauth "github.com/yuyu945/AI-Shopping/internal/platform/auth"
	platformtrace "github.com/yuyu945/AI-Shopping/internal/platform/trace"
	orderpb "github.com/yuyu945/AI-Shopping/services/order-service/gen"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"testing"
)

type recordingConn struct{ ctx context.Context }

func (c *recordingConn) Invoke(ctx context.Context, _ string, _ any, reply any, _ ...grpc.CallOption) error {
	c.ctx = ctx
	reply.(*orderpb.GetCartResponse).Cart = &orderpb.Cart{}
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
