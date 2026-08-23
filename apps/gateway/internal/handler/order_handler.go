package handler

import (
	"encoding/json"
	orderclient "github.com/yuyu945/AI-Shopping/apps/gateway/internal/orderclient"
	orderpb "github.com/yuyu945/AI-Shopping/services/order-service/gen"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"net/http"
	"strconv"
	"strings"
)

type OrderHandler struct{ client orderclient.Client }

func NewOrderHandler(c orderclient.Client) *OrderHandler { return &OrderHandler{c} }
func (h *OrderHandler) Cart() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.client == nil {
			writeOrderError(w, status.Error(codes.Internal, ""))
			return
		}
		if r.Method == http.MethodGet {
			v, e := h.client.GetCart(r.Context(), &orderpb.GetCartRequest{})
			if e != nil {
				writeOrderError(w, e)
				return
			}
			writeJSONValue(w, http.StatusOK, map[string]any{"cart": cartJSON(v.GetCart())})
			return
		}
		var v struct {
			SKUID    uint64 `json:"sku_id"`
			Quantity uint32 `json:"quantity"`
			Selected bool   `json:"selected"`
		}
		if e := json.NewDecoder(r.Body).Decode(&v); e != nil {
			writeOrderError(w, status.Error(codes.InvalidArgument, "invalid request"))
			return
		}
		out, e := h.client.AddCartItem(r.Context(), &orderpb.AddCartItemRequest{SkuId: v.SKUID, Quantity: v.Quantity, Selected: v.Selected})
		if e != nil {
			writeOrderError(w, e)
			return
		}
		writeJSONValue(w, http.StatusOK, map[string]any{"item": cartItemJSON(out.GetItem())})
	}
}
func (h *OrderHandler) CartItem() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, e := strconv.ParseUint(r.PathValue("id"), 10, 64)
		if e != nil || id == 0 {
			writeOrderError(w, status.Error(codes.InvalidArgument, "invalid cart item id"))
			return
		}
		if r.Method == http.MethodDelete {
			_, e = h.client.DeleteCartItem(r.Context(), &orderpb.DeleteCartItemRequest{CartItemId: id})
		} else {
			var v struct {
				Quantity uint32 `json:"quantity"`
				Selected bool   `json:"selected"`
			}
			if e = json.NewDecoder(r.Body).Decode(&v); e == nil {
				_, e = h.client.UpdateCartItem(r.Context(), &orderpb.UpdateCartItemRequest{CartItemId: id, Quantity: v.Quantity, Selected: v.Selected})
			}
		}
		if e != nil {
			writeOrderError(w, e)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
func (h *OrderHandler) Orders() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			out, e := h.client.ListOrders(r.Context(), &orderpb.ListOrdersRequest{})
			if e != nil {
				writeOrderError(w, e)
				return
			}
			v := make([]any, 0, len(out.GetOrders()))
			for _, o := range out.GetOrders() {
				v = append(v, orderJSON(o))
			}
			writeJSONValue(w, http.StatusOK, map[string]any{"orders": v})
			return
		}
		var v struct {
			RequestID string `json:"request_id"`
			AddressID uint64 `json:"address_id"`
		}
		if e := json.NewDecoder(r.Body).Decode(&v); e != nil {
			writeOrderError(w, status.Error(codes.InvalidArgument, "invalid request"))
			return
		}
		out, e := h.client.CreateOrder(r.Context(), &orderpb.CreateOrderRequest{RequestId: v.RequestID, AddressId: v.AddressID})
		if e != nil {
			writeOrderError(w, e)
			return
		}
		writeJSONValue(w, http.StatusOK, map[string]any{"order": orderJSON(out.GetOrder())})
	}
}
func (h *OrderHandler) Order() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		no := r.PathValue("order_no")
		if strings.TrimSpace(no) == "" {
			writeOrderError(w, status.Error(codes.InvalidArgument, "invalid order number"))
			return
		}
		out, e := h.client.GetOrder(r.Context(), &orderpb.GetOrderRequest{OrderNo: no})
		if e != nil {
			writeOrderError(w, e)
			return
		}
		writeJSONValue(w, http.StatusOK, map[string]any{"order": orderJSON(out.GetOrder())})
	}
}
func (h *OrderHandler) WalletPayment() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeOrderError(w, status.Error(codes.InvalidArgument, "invalid request"))
			return
		}
		no := strings.TrimSpace(r.PathValue("order_no"))
		if no == "" {
			writeOrderError(w, status.Error(codes.InvalidArgument, "invalid order number"))
			return
		}
		out, err := h.client.PayWallet(r.Context(), &orderpb.PayWalletRequest{OrderNo: no})
		if err != nil {
			writeOrderError(w, err)
			return
		}
		writeJSONValue(w, http.StatusOK, map[string]any{"order": orderJSON(out.GetOrder())})
	}
}
func cartJSON(v *orderpb.Cart) map[string]any {
	out := map[string]any{"items": []any{}}
	for _, i := range v.GetItems() {
		out["items"] = append(out["items"].([]any), cartItemJSON(i))
	}
	return out
}
func cartItemJSON(v *orderpb.CartItem) map[string]any {
	return map[string]any{"cart_item_id": v.GetCartItemId(), "sku_id": v.GetSkuId(), "quantity": v.GetQuantity(), "selected": v.GetSelected()}
}
func orderJSON(v *orderpb.Order) map[string]any {
	if v == nil {
		return nil
	}
	out := map[string]any{"order_no": v.GetOrderNo(), "request_id": v.GetRequestId(), "status": v.GetStatus(), "total_amount": v.GetTotalAmount(), "paid_amount": v.GetPaidAmount(), "items": []any{}}
	if a := v.GetShippingAddress(); a != nil {
		out["shipping_address"] = map[string]any{"receiver_name": a.GetReceiverName(), "receiver_phone": a.GetReceiverPhone(), "province": a.GetProvince(), "city": a.GetCity(), "district": a.GetDistrict(), "detail": a.GetDetail()}
	}
	for _, i := range v.GetItems() {
		item := map[string]any{"product_id": i.GetProductId(), "sku_id": i.GetSkuId(), "product_title": i.GetProductTitle(), "sku_code": i.GetSkuCode(), "sku_spec_json": json.RawMessage(i.GetSkuSpecJson()), "unit_price": i.GetUnitPrice(), "discount_amount": i.GetDiscountAmount(), "quantity": i.GetQuantity(), "item_amount": i.GetItemAmount(), "candidate_promotions": []any{}}
		for _, promotion := range i.GetCandidatePromotions() {
			item["candidate_promotions"] = append(item["candidate_promotions"].([]any), promotionJSON(promotion))
		}
		if promotion := i.GetAppliedPromotion(); promotion != nil {
			item["applied_promotion"] = promotionJSON(promotion)
		}
		out["items"] = append(out["items"].([]any), item)
	}
	return out
}
func promotionJSON(value *orderpb.PromotionSnapshot) map[string]any {
	result := map[string]any{"promotion_id": value.GetPromotionId(), "rule_type": value.GetRuleType()}
	if value.ThresholdAmount != nil {
		result["threshold_amount"] = value.GetThresholdAmount()
	}
	if value.DiscountAmount != nil {
		result["discount_amount"] = value.GetDiscountAmount()
	}
	return result
}
func writeOrderError(w http.ResponseWriter, e error) {
	code := codes.Internal
	if s, ok := status.FromError(e); ok {
		code = s.Code()
	}
	httpCode := http.StatusInternalServerError
	body := map[string]string{"code": "INTERNAL", "message": "internal server error"}
	switch code {
	case codes.InvalidArgument:
		httpCode = http.StatusBadRequest
		body = map[string]string{"code": "INVALID_ARGUMENT", "message": "invalid request"}
	case codes.Unauthenticated:
		httpCode = http.StatusUnauthorized
		body = map[string]string{"code": "UNAUTHENTICATED", "message": "authentication required"}
	case codes.NotFound:
		httpCode = http.StatusNotFound
		body = map[string]string{"code": "NOT_FOUND", "message": "resource not found"}
	case codes.AlreadyExists:
		httpCode = http.StatusConflict
		body = map[string]string{"code": "IDEMPOTENCY_CONFLICT", "message": "request conflict"}
	case codes.FailedPrecondition:
		httpCode = http.StatusConflict
		body = map[string]string{"code": "OUT_OF_STOCK", "message": "requested inventory is unavailable"}
	case codes.ResourceExhausted:
		httpCode = http.StatusConflict
		body = map[string]string{"code": "INSUFFICIENT_BALANCE", "message": "wallet balance is insufficient"}
	case codes.Aborted:
		httpCode = http.StatusConflict
		body = map[string]string{"code": "PAYMENT_IN_PROGRESS", "message": "payment is in progress"}
	case codes.DeadlineExceeded, codes.Unavailable:
		httpCode = http.StatusGatewayTimeout
		body = map[string]string{"code": "DEPENDENCY_TIMEOUT", "message": "order service timeout"}
	}
	writeJSONValue(w, httpCode, body)
}
