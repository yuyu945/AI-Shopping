package orderpb

import (
	"context"
	"reflect"
	"testing"

	"google.golang.org/grpc"
)

func TestOrderServiceContractExposesAuthenticatedCartAndOrderRPCs(t *testing.T) {
	var _ interface {
		GetCart(context.Context, *GetCartRequest, ...grpc.CallOption) (*GetCartResponse, error)
		AddCartItem(context.Context, *AddCartItemRequest, ...grpc.CallOption) (*CartItemResponse, error)
		UpdateCartItem(context.Context, *UpdateCartItemRequest, ...grpc.CallOption) (*Empty, error)
		DeleteCartItem(context.Context, *DeleteCartItemRequest, ...grpc.CallOption) (*Empty, error)
		CreateOrder(context.Context, *CreateOrderRequest, ...grpc.CallOption) (*OrderResponse, error)
		ListOrders(context.Context, *ListOrdersRequest, ...grpc.CallOption) (*ListOrdersResponse, error)
		GetOrder(context.Context, *GetOrderRequest, ...grpc.CallOption) (*OrderResponse, error)
	} = (OrderServiceClient)(nil)

	if OrderService_GetCart_FullMethodName != "/order.v1.OrderService/GetCart" {
		t.Fatalf("GetCart method = %q", OrderService_GetCart_FullMethodName)
	}
	if OrderService_CreateOrder_FullMethodName != "/order.v1.OrderService/CreateOrder" {
		t.Fatalf("CreateOrder method = %q", OrderService_CreateOrder_FullMethodName)
	}

	request := &CreateOrderRequest{RequestId: "request-1", AddressId: 10}
	if request.GetRequestId() != "request-1" || request.GetAddressId() != 10 {
		t.Fatalf("CreateOrderRequest getters returned unexpected values: %#v", request)
	}

	order := &Order{OrderNo: "ORD-1", TotalAmount: "99.00", PaidAmount: "0.00"}
	if order.GetOrderNo() != "ORD-1" || order.GetTotalAmount() != "99.00" || order.GetPaidAmount() != "0.00" {
		t.Fatalf("order money fields must remain strings: %#v", order)
	}

	for _, request := range []any{
		GetCartRequest{},
		AddCartItemRequest{},
		UpdateCartItemRequest{},
		DeleteCartItemRequest{},
		CreateOrderRequest{},
		ListOrdersRequest{},
		GetOrderRequest{},
	} {
		if _, exists := reflect.TypeOf(request).FieldByName("UserId"); exists {
			t.Fatalf("%T must derive user identity from authentication, not user_id input", request)
		}
	}
}
