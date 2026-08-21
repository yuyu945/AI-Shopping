package catalog

import "encoding/json"

// ProductFilter controls the read-only product listing query.
type ProductFilter struct {
	Keyword    string
	CategoryID uint64
	Page       int
	PageSize   int
}

// ProductSummary is the compact representation returned by product listings.
type ProductSummary struct {
	ID         uint64
	CategoryID uint64
	BrandID    *uint64
	Title      string
	Subtitle   *string
	MinPrice   *string
	StockQty   uint64
}

// ProductDetail contains the active product fields and its read-time data.
type ProductDetail struct {
	ProductSummary
	DetailMarkdown *string
	SKUs           []SKUDetail
	Images         []ImageRef
	Promotions     []PromotionSummary
}

// SKUDetail contains the current price, specification, and MySQL inventory snapshot.
type SKUDetail struct {
	ID        uint64
	SKUCode   string
	SpecJSON  json.RawMessage
	SalePrice string
	Inventory InventorySnapshot
}

// InventorySnapshot is read from MySQL inventory and is never a cache fact.
type InventorySnapshot struct {
	AvailableQty uint64
	Version      uint64
}

// ImageRef identifies a product image in object storage.
type ImageRef struct {
	ID        uint64
	ObjectKey string
	SortNo    uint64
}

// PromotionSummary describes an active promotion attached to a product.
type PromotionSummary struct {
	ID              uint64
	RuleType        string
	ThresholdAmount *string
	DiscountAmount  *string
}
