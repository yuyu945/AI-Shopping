package client

import (
	"context"
	"testing"
	"time"

	"github.com/yuyu945/AI-Shopping/services/order-service/internal/order"
	productpb "github.com/yuyu945/AI-Shopping/services/product-service/gen"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

type reservationRecordingConn struct{ context context.Context }

func (c *reservationRecordingConn) Invoke(ctx context.Context, _ string, _ any, reply any, _ ...grpc.CallOption) error {
	c.context = ctx
	if response, ok := reply.(*productpb.ReservationResponse); ok {
		response.Reservation = &productpb.Reservation{ReservationId: "reservation-1", Status: "RESERVED"}
	}
	return nil
}
func (*reservationRecordingConn) NewStream(context.Context, *grpc.StreamDesc, string, ...grpc.CallOption) (grpc.ClientStream, error) {
	return nil, nil
}

func TestReservationClientForwardsServiceTokenCallerMetadataAndDeadline(t *testing.T) {
	connection := &reservationRecordingConn{}
	client := NewReservationClient(connection, "internal-secret", 100*time.Millisecond)
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer user-token", "trace_id", "trace-1"))
	if _, err := client.ReserveStock(ctx, order.ReserveRequest{ReservationID: "reservation-1", OrderNo: "order-1", PaymentAttemptID: "attempt-1", Items: []order.ReservationItem{{SKUID: 1, Quantity: 2}}, ExpiresAt: time.Now().Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	metadata, _ := metadata.FromOutgoingContext(connection.context)
	for key, want := range map[string]string{"x-ai-shopping-service-token": "internal-secret", "authorization": "Bearer user-token", "trace_id": "trace-1"} {
		if got := metadata.Get(key); len(got) != 1 || got[0] != want {
			t.Fatalf("%s=%v, want %q", key, got, want)
		}
	}
	if _, ok := connection.context.Deadline(); !ok {
		t.Fatal("reservation RPC is missing a deadline")
	}
}
