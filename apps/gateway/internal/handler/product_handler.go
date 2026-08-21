package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	productclient "github.com/yuyu945/AI-Shopping/apps/gateway/internal/productclient"
	productpb "github.com/yuyu945/AI-Shopping/services/product-service/gen"
	gozerotrace "github.com/zeromicro/go-zero/core/trace"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ProductHandler struct{ client productclient.Client }

func NewProductHandler(client productclient.Client) *ProductHandler {
	return &ProductHandler{client: client}
}

func (h *ProductHandler) List() http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		preserveProductTrace(writer, request)
		query := request.URL.Query()
		page, err := queryUint(query.Get("page"), 1)
		if err != nil || page == 0 {
			writeProductError(writer, status.Error(codes.InvalidArgument, "page must be a positive integer"))
			return
		}
		pageSize, err := queryUint(query.Get("page_size"), 20)
		if err != nil || pageSize == 0 || pageSize > 100 {
			writeProductError(writer, status.Error(codes.InvalidArgument, "page_size must be between 1 and 100"))
			return
		}
		categoryID, err := queryUint(query.Get("category_id"), 0)
		if err != nil {
			writeProductError(writer, status.Error(codes.InvalidArgument, "category_id must be a positive integer"))
			return
		}
		if h.client == nil {
			writeProductError(writer, status.Error(codes.Internal, "internal server error"))
			return
		}
		result, err := h.client.ListProducts(request.Context(), &productpb.ListProductsRequest{Keyword: query.Get("keyword"), CategoryId: categoryID, Page: uint32(page), PageSize: uint32(pageSize)})
		if err != nil {
			writeProductError(writer, err)
			return
		}
		writeJSONValue(writer, http.StatusOK, map[string]any{"products": mapProductSummariesJSON(result.GetProducts())})
	}
}

func (h *ProductHandler) Get() http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		preserveProductTrace(writer, request)
		idText := request.PathValue("id")
		if idText == "" {
			idText = pathProductID(request.URL.Path)
		}
		productID, err := strconv.ParseUint(idText, 10, 64)
		if err != nil || productID == 0 {
			writeProductError(writer, status.Error(codes.InvalidArgument, "product id must be a positive integer"))
			return
		}
		var skuID *uint64
		if value := request.URL.Query().Get("sku_id"); value != "" {
			parsed, parseErr := strconv.ParseUint(value, 10, 64)
			if parseErr != nil || parsed == 0 {
				writeProductError(writer, status.Error(codes.InvalidArgument, "sku_id must be a positive integer"))
				return
			}
			skuID = &parsed
		}
		if h.client == nil {
			writeProductError(writer, status.Error(codes.Internal, "internal server error"))
			return
		}
		requestMsg := &productpb.GetProductRequest{ProductId: productID, SkuId: skuID}
		result, err := h.client.GetProduct(request.Context(), requestMsg)
		if err != nil {
			writeProductError(writer, err)
			return
		}
		writeJSONValue(writer, http.StatusOK, map[string]any{"product": mapProductJSON(result.GetProduct())})
	}
}

func preserveProductTrace(writer http.ResponseWriter, request *http.Request) {
	traceID := request.Header.Get("X-Trace-ID")
	if !isValidTraceID(traceID) {
		traceID = gozerotrace.TraceIDFromContext(request.Context())
	}
	if isValidTraceID(traceID) {
		writer.Header().Set("X-Trace-ID", traceID)
	}
}

func queryUint(value string, fallback uint64) (uint64, error) {
	if value == "" {
		return fallback, nil
	}
	return strconv.ParseUint(value, 10, 64)
}

func pathProductID(path string) string {
	const prefix = "/api/v1/products/"
	if len(path) <= len(prefix) || path[:len(prefix)] != prefix {
		return ""
	}
	return path[len(prefix):]
}

func mapProductSummariesJSON(items []*productpb.ProductSummary) []map[string]any {
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		value := map[string]any{"product_id": item.GetProductId(), "category_id": item.GetCategoryId(), "title": item.GetTitle(), "stock_qty": item.GetStockQty(), "stock_status": item.GetStockStatus()}
		if item.Subtitle != nil {
			value["subtitle"] = item.GetSubtitle()
		}
		if item.MinSalePrice != nil {
			value["min_sale_price"] = item.GetMinSalePrice()
		}
		result = append(result, value)
	}
	return result
}

func mapProductJSON(item *productpb.Product) map[string]any {
	if item == nil {
		return nil
	}
	value := map[string]any{"product_id": item.GetProductId(), "category_id": item.GetCategoryId(), "title": item.GetTitle(), "skus": mapSKUsJSON(item.GetSkus()), "images": mapImagesJSON(item.GetImages()), "promotions": mapPromotionsJSON(item.GetPromotions())}
	if item.Subtitle != nil {
		value["subtitle"] = item.GetSubtitle()
	}
	if item.DetailMarkdown != nil {
		value["detail_markdown"] = item.GetDetailMarkdown()
	}
	return value
}

func mapImagesJSON(items []*productpb.ImageRef) []map[string]any {
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if item != nil {
			result = append(result, map[string]any{"image_id": item.GetImageId(), "object_key": item.GetObjectKey(), "sort_no": item.GetSortNo()})
		}
	}
	return result
}

func mapPromotionsJSON(items []*productpb.PromotionSummary) []map[string]any {
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		value := map[string]any{"promotion_id": item.GetPromotionId(), "rule_type": item.GetRuleType(), "discount_amount": item.GetDiscountAmount()}
		if item.ThresholdAmount != nil {
			value["threshold_amount"] = item.GetThresholdAmount()
		}
		result = append(result, value)
	}
	return result
}

func mapSKUsJSON(items []*productpb.Sku) []map[string]any {
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		result = append(result, map[string]any{"sku_id": item.GetSkuId(), "sku_code": item.GetSkuCode(), "specs": item.GetSpecs(), "sale_price": item.GetSalePrice(), "stock_qty": item.GetStockQty(), "stock_status": item.GetStockStatus()})
	}
	return result
}

func writeProductError(writer http.ResponseWriter, err error) {
	code := codes.Internal
	if statusErr, ok := status.FromError(err); ok {
		code = statusErr.Code()
	}
	statusCode := http.StatusInternalServerError
	stableCode := "INTERNAL"
	message := "internal server error"
	switch code {
	case codes.InvalidArgument:
		statusCode, stableCode, message = http.StatusBadRequest, "INVALID_ARGUMENT", "invalid request"
	case codes.NotFound:
		statusCode, stableCode, message = http.StatusNotFound, "NOT_FOUND", "product not found"
	case codes.DeadlineExceeded, codes.Unavailable:
		statusCode, stableCode, message = http.StatusGatewayTimeout, "DEPENDENCY_TIMEOUT", "product service timeout"
	}
	writeJSONValue(writer, statusCode, map[string]string{"code": stableCode, "message": message})
}

func writeJSONValue(writer http.ResponseWriter, statusCode int, body any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(statusCode)
	_ = jsonNewEncoder(writer).Encode(body)
}

var jsonNewEncoder = func(writer http.ResponseWriter) *json.Encoder { return json.NewEncoder(writer) }
