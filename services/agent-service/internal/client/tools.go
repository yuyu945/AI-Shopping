package client

import (
	"context"
	"errors"
	"time"

	"github.com/yuyu945/AI-Shopping/services/agent-service/internal/agent"
	knowledgepb "github.com/yuyu945/AI-Shopping/services/knowledge-service/gen"
	productpb "github.com/yuyu945/AI-Shopping/services/product-service/gen"
	userpb "github.com/yuyu945/AI-Shopping/services/user-service/gen"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// ProductClient adapts product-service gRPC reads for Agent Tools.
type ProductClient struct {
	client  productpb.ProductServiceClient
	timeout time.Duration
}

// NewProductClient constructs a product Tool client.
func NewProductClient(conn grpc.ClientConnInterface, timeout time.Duration) *ProductClient {
	return &ProductClient{client: productpb.NewProductServiceClient(conn), timeout: timeout}
}

func (c *ProductClient) ListProducts(ctx context.Context, request agent.ProductSearchRequest) (agent.ProductSearchResult, error) {
	callCtx, cancel := context.WithTimeout(ctx, c.timeoutOrDefault())
	defer cancel()
	resp, err := c.client.ListProducts(callCtx, &productpb.ListProductsRequest{
		Keyword: request.Keyword, CategoryId: request.CategoryID, Page: 1, PageSize: uint32(request.Limit),
	})
	if err != nil {
		return agent.ProductSearchResult{}, mapDependencyError(err)
	}
	out := agent.ProductSearchResult{Products: make([]agent.ProductSearchItem, 0, len(resp.GetProducts()))}
	for _, item := range resp.GetProducts() {
		out.Products = append(out.Products, agent.ProductSearchItem{
			ProductID: item.GetProductId(), CategoryID: item.GetCategoryId(), Title: item.GetTitle(),
			MinSalePrice: item.GetMinSalePrice(), StockStatus: item.GetStockStatus(),
		})
	}
	return out, nil
}

func (c *ProductClient) GetCheckoutSKUs(ctx context.Context, skuIDs []uint64) ([]agent.CheckoutSKU, error) {
	callCtx, cancel := context.WithTimeout(ctx, c.timeoutOrDefault())
	defer cancel()
	resp, err := c.client.GetCheckoutSKUs(callCtx, &productpb.CheckoutSKUsRequest{SkuIds: append([]uint64(nil), skuIDs...)})
	if err != nil {
		return nil, mapDependencyError(err)
	}
	out := make([]agent.CheckoutSKU, 0, len(resp.GetSkus()))
	for _, item := range resp.GetSkus() {
		sku := agent.CheckoutSKU{
			ProductID: item.GetProductId(), SKUID: item.GetSkuId(), ProductTitle: item.GetProductTitle(),
			SKUCode: item.GetSkuCode(), SpecJSON: append([]byte(nil), item.GetSpecJson()...),
			SalePrice: item.GetSalePrice(), Saleable: item.GetSaleable(),
		}
		for _, promotion := range item.GetPromotions() {
			sku.Promotions = append(sku.Promotions, agent.PromotionSummary{
				PromotionID: promotion.GetPromotionId(), RuleType: promotion.GetRuleType(),
				ThresholdAmount: promotion.GetThresholdAmount(), DiscountAmount: promotion.GetDiscountAmount(),
			})
		}
		out = append(out, sku)
	}
	return out, nil
}

func (c *ProductClient) timeoutOrDefault() time.Duration {
	if c.timeout <= 0 {
		return 2 * time.Second
	}
	return c.timeout
}

// UserClient adapts user-service gRPC reads for Agent Tools.
type UserClient struct {
	client  userpb.UserServiceClient
	timeout time.Duration
}

// NewUserClient constructs a user Tool client.
func NewUserClient(conn grpc.ClientConnInterface, timeout time.Duration) *UserClient {
	return &UserClient{client: userpb.NewUserServiceClient(conn), timeout: timeout}
}

func (c *UserClient) GetMyProfile(ctx context.Context) (agent.UserProfile, error) {
	callCtx, cancel := context.WithTimeout(outgoingAuthContext(ctx), c.timeoutOrDefault())
	defer cancel()
	resp, err := c.client.GetMyProfile(callCtx, &userpb.GetMyProfileRequest{})
	if err != nil {
		return agent.UserProfile{}, mapDependencyError(err)
	}
	profile := resp.GetProfile()
	if profile == nil {
		return agent.UserProfile{}, nil
	}
	user := profile.GetUser()
	return agent.UserProfile{
		UserID: user.GetUserId(), Email: user.GetEmail(), PreferenceJSON: append([]byte(nil), profile.GetPreferenceJson()...),
		BudgetMin: profile.GetBudgetMin(), BudgetMax: profile.GetBudgetMax(), ProfileVersion: profile.GetProfileVersion(),
	}, nil
}

func (c *UserClient) timeoutOrDefault() time.Duration {
	if c.timeout <= 0 {
		return 2 * time.Second
	}
	return c.timeout
}

// KnowledgeClient adapts knowledge-service gRPC reads for Agent Tools.
type KnowledgeClient struct {
	client  knowledgepb.KnowledgeServiceClient
	timeout time.Duration
}

// NewKnowledgeClient constructs a knowledge Tool client.
func NewKnowledgeClient(conn grpc.ClientConnInterface, timeout time.Duration) *KnowledgeClient {
	return &KnowledgeClient{client: knowledgepb.NewKnowledgeServiceClient(conn), timeout: timeout}
}

func (c *KnowledgeClient) SearchProductKnowledge(ctx context.Context, request agent.KnowledgeSearchRequest) (agent.KnowledgeSearchResult, error) {
	callCtx, cancel := context.WithTimeout(ctx, c.timeoutOrDefault())
	defer cancel()
	resp, err := c.client.SearchProductKnowledge(callCtx, &knowledgepb.SearchProductKnowledgeRequest{
		ProductId: request.ProductID, Query: request.Query, DocTypes: append([]string(nil), request.DocTypes...), TopK: uint32(request.TopK),
	})
	if err != nil {
		return agent.KnowledgeSearchResult{}, mapDependencyError(err)
	}
	out := agent.KnowledgeSearchResult{FallbackReason: resp.GetFallbackReason(), Snippets: make([]agent.KnowledgeSnippet, 0, len(resp.GetSnippets()))}
	for _, item := range resp.GetSnippets() {
		out.Snippets = append(out.Snippets, agent.KnowledgeSnippet{
			ChunkID: item.GetChunkId(), DocumentNo: item.GetDocumentNo(), ProductID: item.GetProductId(),
			DocType: item.GetDocType(), Version: item.GetVersion(), Section: item.GetSection(),
			SourcePage: item.GetSourcePage(), Content: item.GetContent(), Score: item.GetScore(),
		})
	}
	return out, nil
}

func (c *KnowledgeClient) timeoutOrDefault() time.Duration {
	if c.timeout <= 0 {
		return 2 * time.Second
	}
	return c.timeout
}

func outgoingAuthContext(ctx context.Context) context.Context {
	values := metadata.ValueFromIncomingContext(ctx, "authorization")
	if len(values) != 1 {
		return ctx
	}
	return metadata.AppendToOutgoingContext(ctx, "authorization", values[0])
}

func mapDependencyError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) || status.Code(err) == codes.DeadlineExceeded {
		return agent.ErrDependencyTimeout
	}
	if status.Code(err) == codes.NotFound {
		return agent.ErrCheckoutSKUUnavailable
	}
	return err
}
