package catalog

import "encoding/json"

// ProductSummaryDTO is the application read model exposed by ProductService.
// It intentionally does not alias repository/persistence structures.
type ProductSummaryDTO struct {
	ProductID    uint64
	CategoryID   uint64
	Title        string
	Subtitle     *string
	MinSalePrice *string
	StockQty     uint64
}

// ProductDetailDTO is the application read model exposed by ProductService.
type ProductDetailDTO struct {
	ProductID      uint64
	CategoryID     uint64
	Title          string
	Subtitle       *string
	DetailMarkdown *string
	SKUs           []SKUDetailDTO
	Images         []ImageRefDTO
	Promotions     []PromotionSummaryDTO
}

type SKUDetailDTO struct {
	SKUID     uint64
	SKUCode   string
	Specs     map[string]string
	SalePrice string
	StockQty  uint64
}

type ImageRefDTO struct {
	ImageID   uint64
	ObjectKey string
	SortNo    uint64
}

type PromotionSummaryDTO struct {
	PromotionID     uint64
	RuleType        string
	ThresholdAmount *string
	DiscountAmount  *string
}

func mapProductSummaries(rows []ProductSummary) []ProductSummaryDTO {
	result := make([]ProductSummaryDTO, 0, len(rows))
	for _, row := range rows {
		result = append(result, ProductSummaryDTO{
			ProductID: row.ID, CategoryID: row.CategoryID, Title: row.Title,
			Subtitle: cloneStringPtr(row.Subtitle), MinSalePrice: cloneStringPtr(row.MinPrice), StockQty: row.StockQty,
		})
	}
	return result
}

func mapProductDetail(detail ProductDetail) ProductDetailDTO {
	result := ProductDetailDTO{
		ProductID: detail.ID, CategoryID: detail.CategoryID, Title: detail.Title,
		Subtitle: cloneStringPtr(detail.Subtitle), DetailMarkdown: cloneStringPtr(detail.DetailMarkdown),
	}
	for _, sku := range detail.SKUs {
		result.SKUs = append(result.SKUs, SKUDetailDTO{SKUID: sku.ID, SKUCode: sku.SKUCode, Specs: mapSpecs(sku.SpecJSON), SalePrice: sku.SalePrice, StockQty: sku.Inventory.AvailableQty})
	}
	for _, image := range detail.Images {
		result.Images = append(result.Images, ImageRefDTO{ImageID: image.ID, ObjectKey: image.ObjectKey, SortNo: image.SortNo})
	}
	for _, promotion := range detail.Promotions {
		result.Promotions = append(result.Promotions, PromotionSummaryDTO{PromotionID: promotion.ID, RuleType: promotion.RuleType, ThresholdAmount: cloneStringPtr(promotion.ThresholdAmount), DiscountAmount: cloneStringPtr(promotion.DiscountAmount)})
	}
	return result
}

func mapSpecs(raw json.RawMessage) map[string]string {
	specs := map[string]string{}
	_ = json.Unmarshal(raw, &specs)
	return specs
}
