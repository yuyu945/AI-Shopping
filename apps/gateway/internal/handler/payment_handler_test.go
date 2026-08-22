package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	orderpb "github.com/yuyu945/AI-Shopping/services/order-service/gen"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestOrderPaymentHandlerReturnsPaymentSnapshot(t *testing.T) {
	client := &fakeOrderClient{pay: func(_ context.Context, req *orderpb.PayWalletRequest) (*orderpb.OrderResponse, error) {
		if req.GetOrderNo() != "ORD-1" {
			t.Fatalf("order_no=%q", req.GetOrderNo())
		}
		return &orderpb.OrderResponse{Order: &orderpb.Order{OrderNo: "ORD-1", Status: "PAID", TotalAmount: "10.00", PaidAmount: "10.00", Items: []*orderpb.OrderItem{{SkuId: 1, CandidatePromotions: []*orderpb.PromotionSnapshot{{PromotionId: 9, RuleType: "AMOUNT_OFF"}}}}}}, nil
	}}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/orders/ORD-1/payments/wallet", nil)
	r.SetPathValue("order_no", "ORD-1")
	NewOrderHandler(client).WalletPayment().ServeHTTP(w, r)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"status":"PAID"`) || !strings.Contains(w.Body.String(), `"candidate_promotions"`) {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestOrderPaymentHandlerMapsStableErrors(t *testing.T) {
	cases := []struct {
		code codes.Code
		want int
		body string
	}{
		{codes.FailedPrecondition, http.StatusConflict, "OUT_OF_STOCK"},
		{codes.ResourceExhausted, http.StatusConflict, "INSUFFICIENT_BALANCE"},
		{codes.Aborted, http.StatusConflict, "PAYMENT_IN_PROGRESS"},
		{codes.AlreadyExists, http.StatusConflict, "IDEMPOTENCY_CONFLICT"},
		{codes.DeadlineExceeded, http.StatusGatewayTimeout, "DEPENDENCY_TIMEOUT"},
	}
	for _, tc := range cases {
		t.Run(tc.body, func(t *testing.T) {
			h := NewOrderHandler(&fakeOrderClient{pay: func(context.Context, *orderpb.PayWalletRequest) (*orderpb.OrderResponse, error) {
				return nil, status.Error(tc.code, "sensitive error")
			}})
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodPost, "/", nil)
			r.SetPathValue("order_no", "ORD-1")
			h.WalletPayment().ServeHTTP(w, r)
			if w.Code != tc.want || !strings.Contains(w.Body.String(), tc.body) || strings.Contains(w.Body.String(), "sensitive") {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}
