package orderpb

import (
	"context"
	"reflect"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/reflect/protoreflect"
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
	assertStringFields(t, (&Order{}).ProtoReflect().Descriptor(), "total_amount", "paid_amount")
	assertStringFields(t, (&OrderItem{}).ProtoReflect().Descriptor(), "unit_price", "discount_amount", "item_amount")
	assertStringFields(t, (&PromotionSnapshot{}).ProtoReflect().Descriptor(), "threshold_amount", "discount_amount")

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

func TestOrderItemPromotionSnapshotContract(t *testing.T) {
	firstCandidate := &PromotionSnapshot{PromotionId: 1, RuleType: "DIRECT", DiscountAmount: stringRef("5.00")}
	appliedCandidate := &PromotionSnapshot{PromotionId: 2, RuleType: "DIRECT", DiscountAmount: stringRef("10.00")}
	item := &OrderItem{
		CandidatePromotions: []*PromotionSnapshot{firstCandidate, appliedCandidate},
		AppliedPromotion:    appliedCandidate,
	}
	if len(item.GetCandidatePromotions()) != 2 || item.GetAppliedPromotion().GetPromotionId() != 2 {
		t.Fatalf("candidate and applied promotions = %#v", item)
	}
	field := item.ProtoReflect().Descriptor().Fields().ByName("applied_promotion")
	if field == nil || !item.ProtoReflect().Has(field) {
		t.Fatal("applied_promotion must be present when a promotion is applied")
	}

	withoutPromotion := &OrderItem{CandidatePromotions: []*PromotionSnapshot{firstCandidate}}
	if withoutPromotion.GetAppliedPromotion() != nil || withoutPromotion.ProtoReflect().Has(field) {
		t.Fatal("applied_promotion must be unset when no promotion is applied")
	}
}

func assertStringFields(t *testing.T, descriptor protoreflect.MessageDescriptor, names ...protoreflect.Name) {
	t.Helper()
	for _, name := range names {
		field := descriptor.Fields().ByName(name)
		if field == nil || field.Kind() != protoreflect.StringKind {
			t.Fatalf("%s.%s must be a string field", descriptor.FullName(), name)
		}
	}
}

func stringRef(value string) *string { return &value }
