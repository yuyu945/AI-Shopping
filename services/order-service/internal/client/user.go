package client

import (
	"context"
	platformtrace "github.com/yuyu945/AI-Shopping/internal/platform/trace"
	"github.com/yuyu945/AI-Shopping/services/order-service/internal/order"
	userpb "github.com/yuyu945/AI-Shopping/services/user-service/gen"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"time"
)

type UserClient struct {
	client  userpb.UserServiceClient
	timeout time.Duration
}

func NewUserClient(conn grpc.ClientConnInterface, timeout time.Duration) *UserClient {
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	return &UserClient{userpb.NewUserServiceClient(conn), timeout}
}
func (c *UserClient) GetAddress(ctx context.Context, _ uint64, addressID uint64) (order.AddressSnapshot, error) {
	call, cancel := context.WithTimeout(outgoing(ctx), c.timeout)
	defer cancel()
	out, e := c.client.GetMyAddressSnapshot(call, &userpb.GetMyAddressRequest{AddressId: addressID})
	if e != nil {
		return order.AddressSnapshot{}, dependencyError(e)
	}
	v := out.GetAddress()
	if v == nil {
		return order.AddressSnapshot{}, order.ErrNotFound
	}
	return order.AddressSnapshot{AddressID: v.GetAddressId(), ReceiverName: v.GetReceiverName(), ReceiverPhone: v.GetReceiverPhone(), Province: v.GetProvince(), City: v.GetCity(), District: v.GetDistrict(), Detail: v.GetDetail()}, nil
}
func outgoing(ctx context.Context) context.Context {
	md := metadata.MD{}
	if in, ok := metadata.FromIncomingContext(ctx); ok {
		for _, key := range []string{"authorization", "trace_id"} {
			if values := in.Get(key); len(values) > 0 {
				md[key] = append([]string(nil), values...)
			}
		}
	}
	if id := platformtrace.TraceID(ctx); id != "" && len(md.Get("trace_id")) == 0 {
		md.Set("trace_id", id)
	}
	return metadata.NewOutgoingContext(ctx, md)
}
func dependencyError(e error) error {
	switch status.Code(e) {
	case codes.NotFound:
		return order.ErrNotFound
	case codes.DeadlineExceeded, codes.Unavailable:
		return context.DeadlineExceeded
	default:
		return e
	}
}
