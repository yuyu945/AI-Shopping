package agent

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ErrDependencyTimeout reports a Tool dependency deadline.
var ErrDependencyTimeout = errors.New("DEPENDENCY_TIMEOUT")

// ErrToolFailed reports a controlled Tool dependency failure.
var ErrToolFailed = errors.New("TOOL_FAILED")

// ProductClient is the product-service read surface used by Agent Tools.
type ProductClient interface {
	ListProducts(context.Context, ProductSearchRequest) (ProductSearchResult, error)
	GetCheckoutSKUs(context.Context, []uint64) ([]CheckoutSKU, error)
}

// UserClient is the user-service read surface used by Agent Tools.
type UserClient interface {
	GetMyProfile(context.Context) (UserProfile, error)
}

// KnowledgeClient is the knowledge-service read surface used by Agent Tools.
type KnowledgeClient interface {
	SearchProductKnowledge(context.Context, KnowledgeSearchRequest) (KnowledgeSearchResult, error)
}

// ProductSearchRequest is the normalized search_products dependency request.
type ProductSearchRequest struct {
	Keyword    string
	CategoryID uint64
	BudgetMin  string
	BudgetMax  string
	Limit      int
}

// ProductSearchResult is the stable search_products Tool output.
type ProductSearchResult struct {
	Products []ProductSearchItem `json:"products"`
}

// ProductSearchItem is a product summary returned to the model.
type ProductSearchItem struct {
	ProductID    uint64 `json:"product_id"`
	CategoryID   uint64 `json:"category_id"`
	Title        string `json:"title"`
	MinSalePrice string `json:"min_sale_price,omitempty"`
	StockStatus  string `json:"stock_status"`
}

// CheckoutSKU is the normalized product checkout SKU snapshot.
type CheckoutSKU struct {
	ProductID    uint64             `json:"product_id"`
	SKUID        uint64             `json:"sku_id"`
	ProductTitle string             `json:"product_title"`
	SKUCode      string             `json:"sku_code"`
	SpecJSON     []byte             `json:"spec_json,omitempty"`
	SalePrice    string             `json:"sale_price"`
	Saleable     bool               `json:"saleable"`
	Promotions   []PromotionSummary `json:"promotions,omitempty"`
}

// PromotionSummary is the normalized promotion summary visible to Agent Tools.
type PromotionSummary struct {
	PromotionID     uint64 `json:"promotion_id"`
	RuleType        string `json:"rule_type"`
	ThresholdAmount string `json:"threshold_amount,omitempty"`
	DiscountAmount  string `json:"discount_amount,omitempty"`
}

// PriceStockResult is the stable get_price_stock Tool output.
type PriceStockResult struct {
	SKUs []CheckoutSKU `json:"skus"`
}

// DiscountResult is the stable get_discount Tool output.
type DiscountResult struct {
	UserID uint64         `json:"user_id"`
	Items  []DiscountItem `json:"items"`
}

// DiscountItem contains promotions associated with one SKU.
type DiscountItem struct {
	SKUID      uint64             `json:"sku_id"`
	ProductID  uint64             `json:"product_id"`
	SalePrice  string             `json:"sale_price"`
	Promotions []PromotionSummary `json:"promotions"`
}

// UserProfile is the stable get_user_profile Tool output.
type UserProfile struct {
	UserID         uint64 `json:"user_id"`
	Email          string `json:"email"`
	PreferenceJSON []byte `json:"preference_json,omitempty"`
	BudgetMin      string `json:"budget_min,omitempty"`
	BudgetMax      string `json:"budget_max,omitempty"`
	ProfileVersion uint64 `json:"profile_version"`
}

// KnowledgeSearchRequest is the normalized knowledge-service search request.
type KnowledgeSearchRequest struct {
	ProductID uint64
	Query     string
	DocTypes  []string
	TopK      int
}

// KnowledgeSearchResult is the stable search_product_knowledge Tool output.
type KnowledgeSearchResult struct {
	Snippets       []KnowledgeSnippet `json:"snippets"`
	FallbackReason string             `json:"fallback_reason,omitempty"`
}

// KnowledgeSnippet is a retrieved product-document chunk.
type KnowledgeSnippet struct {
	ChunkID    uint64  `json:"chunk_id"`
	DocumentNo string  `json:"document_no"`
	ProductID  uint64  `json:"product_id"`
	DocType    string  `json:"doc_type"`
	Version    uint32  `json:"version"`
	Section    string  `json:"section,omitempty"`
	SourcePage uint32  `json:"source_page,omitempty"`
	Content    string  `json:"content"`
	Score      float64 `json:"score"`
}

// ToolResult is the controlled result of one Tool execution.
type ToolResult struct {
	ToolName string
	UserID   uint64
	Output   any
}

// ToolExecutor executes validated Agent Tool invocations.
type ToolExecutor struct {
	registry  *ToolRegistry
	product   ProductClient
	user      UserClient
	knowledge KnowledgeClient
}

// NewToolExecutor constructs a Tool executor from runtime clients.
func NewToolExecutor(registry *ToolRegistry, product ProductClient, user UserClient, knowledge KnowledgeClient) *ToolExecutor {
	return &ToolExecutor{registry: registry, product: product, user: user, knowledge: knowledge}
}

// Execute runs a previously validated Tool invocation with the Tool timeout.
func (e *ToolExecutor) Execute(ctx context.Context, invocation ToolInvocation) (ToolResult, error) {
	definition, ok := e.registry.Definition(invocation.Name)
	if !ok {
		return ToolResult{}, ErrUnknownTool
	}
	timeout := definition.Timeout
	if timeout <= 0 {
		timeout = time.Second
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	output, err := e.execute(callCtx, invocation)
	if err != nil {
		return ToolResult{}, mapToolDependencyError(invocation.Name, err)
	}
	return ToolResult{ToolName: invocation.Name, UserID: invocation.UserID, Output: output}, nil
}

func (e *ToolExecutor) execute(ctx context.Context, invocation ToolInvocation) (any, error) {
	switch invocation.Name {
	case ToolSearchProducts:
		args, ok := invocation.Args.(SearchProductsArgs)
		if !ok {
			return nil, ErrInvalidToolArgument
		}
		if e.product == nil {
			return nil, fmt.Errorf("%w: product client unavailable", ErrToolFailed)
		}
		return e.product.ListProducts(ctx, ProductSearchRequest{
			Keyword: args.Keyword, CategoryID: args.CategoryID, BudgetMin: args.BudgetMin,
			BudgetMax: args.BudgetMax, Limit: args.Limit,
		})
	case ToolGetUserProfile:
		if e.user == nil {
			return nil, fmt.Errorf("%w: user client unavailable", ErrToolFailed)
		}
		return e.user.GetMyProfile(ctx)
	case ToolGetPriceStock:
		args, ok := invocation.Args.(GetPriceStockArgs)
		if !ok {
			return nil, ErrInvalidToolArgument
		}
		return e.getPriceStock(ctx, args.SKUIDs)
	case ToolGetDiscount:
		args, ok := invocation.Args.(GetDiscountArgs)
		if !ok {
			return nil, ErrInvalidToolArgument
		}
		return e.getDiscount(ctx, invocation.UserID, args.SKUIDs)
	case ToolSearchProductKnowledge:
		args, ok := invocation.Args.(SearchProductKnowledgeArgs)
		if !ok {
			return nil, ErrInvalidToolArgument
		}
		if e.knowledge == nil {
			return nil, fmt.Errorf("%w: knowledge client unavailable", ErrToolFailed)
		}
		return e.knowledge.SearchProductKnowledge(ctx, KnowledgeSearchRequest{
			ProductID: args.ProductID, Query: args.Query, DocTypes: append([]string(nil), args.DocTypes...), TopK: args.TopK,
		})
	default:
		return nil, ErrUnknownTool
	}
}

func (e *ToolExecutor) getPriceStock(ctx context.Context, skuIDs []uint64) (PriceStockResult, error) {
	if e.product == nil {
		return PriceStockResult{}, fmt.Errorf("%w: product client unavailable", ErrToolFailed)
	}
	skus, err := e.product.GetCheckoutSKUs(ctx, append([]uint64(nil), skuIDs...))
	if err != nil {
		return PriceStockResult{}, err
	}
	return PriceStockResult{SKUs: skus}, nil
}

func (e *ToolExecutor) getDiscount(ctx context.Context, userID uint64, skuIDs []uint64) (DiscountResult, error) {
	if e.product == nil {
		return DiscountResult{}, fmt.Errorf("%w: product client unavailable", ErrToolFailed)
	}
	skus, err := e.product.GetCheckoutSKUs(ctx, append([]uint64(nil), skuIDs...))
	if err != nil {
		return DiscountResult{}, err
	}
	items := make([]DiscountItem, 0, len(skus))
	for _, sku := range skus {
		items = append(items, DiscountItem{
			SKUID: sku.SKUID, ProductID: sku.ProductID, SalePrice: sku.SalePrice,
			Promotions: append([]PromotionSummary(nil), sku.Promotions...),
		})
	}
	return DiscountResult{UserID: userID, Items: items}, nil
}

func mapToolDependencyError(toolName string, err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%w: %s", ErrDependencyTimeout, toolName)
	}
	if errors.Is(err, ErrUnknownTool) || errors.Is(err, ErrInvalidToolArgument) || errors.Is(err, ErrToolFailed) {
		return err
	}
	return fmt.Errorf("%w: %s", ErrToolFailed, toolName)
}
