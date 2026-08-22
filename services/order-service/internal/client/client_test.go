package client

import (
	"context"
	userpb "github.com/yuyu945/AI-Shopping/services/user-service/gen"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"testing"
	"time"

	productpb "github.com/yuyu945/AI-Shopping/services/product-service/gen"
)

type recordingConn struct{ ctx context.Context }

func (c *recordingConn) Invoke(ctx context.Context, _ string, _ any, reply any, _ ...grpc.CallOption) error {
	c.ctx = ctx
	if r, ok := reply.(*userpb.AddressResponse); ok {
		r.Address = &userpb.Address{AddressId: 9, ReceiverName: "Ada", ReceiverPhone: "138", Province: "P", City: "C", District: "D", Detail: "X"}
	}
	if _, ok := reply.(*productpb.CheckoutSKUsResponse); ok {
		return nil
	}
	return nil
}

func TestProductClientForwardsMetadataAndBoundsDeadline(t *testing.T) {
	conn := &recordingConn{}
	client := NewProductClient(conn, 100*time.Millisecond)
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer token", "trace_id", "trace-1"))
	if _, err := client.GetProducts(ctx, []uint64{9}); err != nil {
		t.Fatal(err)
	}
	md, _ := metadata.FromOutgoingContext(conn.ctx)
	if got := md.Get("authorization"); len(got) != 1 || got[0] != "Bearer token" {
		t.Fatalf("authorization=%v", got)
	}
	if got := md.Get("trace_id"); len(got) != 1 || got[0] != "trace-1" {
		t.Fatalf("trace=%v", got)
	}
	if _, ok := conn.ctx.Deadline(); !ok {
		t.Fatal("outgoing call missing deadline")
	}
}
func (*recordingConn) NewStream(context.Context, *grpc.StreamDesc, string, ...grpc.CallOption) (grpc.ClientStream, error) {
	return nil, nil
}
func TestUserClientForwardsMetadataAndBoundsDeadline(t *testing.T) {
	conn := &recordingConn{}
	c := NewUserClient(conn, 100*time.Millisecond)
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer token", "trace_id", "trace-1"))
	v, e := c.GetAddress(ctx, 7, 9)
	if e != nil || v.AddressID != 9 {
		t.Fatalf("GetAddress=%#v,%v", v, e)
	}
	md, _ := metadata.FromOutgoingContext(conn.ctx)
	if got := md.Get("authorization"); len(got) != 1 || got[0] != "Bearer token" {
		t.Fatalf("authorization=%v", got)
	}
	if got := md.Get("trace_id"); len(got) != 1 || got[0] != "trace-1" {
		t.Fatalf("trace=%v", got)
	}
	if _, ok := conn.ctx.Deadline(); !ok {
		t.Fatal("outgoing call missing deadline")
	}
}
