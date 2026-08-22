package orderclient

import (
	"context"
	platformauth "github.com/yuyu945/AI-Shopping/internal/platform/auth"
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
	ctx := platformauth.ContextWithBearer(context.Background(), "Bearer token")
	_, e := NewGRPCClient(conn).GetCart(ctx, &orderpb.GetCartRequest{})
	if e != nil {
		t.Fatal(e)
	}
	md, _ := metadata.FromOutgoingContext(conn.ctx)
	if got := md.Get("authorization"); len(got) != 1 || got[0] != "Bearer token" {
		t.Fatalf("authorization=%v", got)
	}
}
