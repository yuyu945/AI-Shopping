package agent

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSearchProductsToolCallsProductService(t *testing.T) {
	product := &fakeProductClient{
		searchResult: ProductSearchResult{Products: []ProductSearchItem{{
			ProductID: 1001, CategoryID: 10, Title: "轻薄笔记本", MinSalePrice: "4999.00", StockStatus: "IN_STOCK",
		}}},
	}
	executor := NewToolExecutor(NewDefaultToolRegistry(time.Second), product, nil, nil)

	result, err := executor.Execute(context.Background(), ToolInvocation{
		Name:   ToolSearchProducts,
		UserID: 42,
		Args: SearchProductsArgs{
			Keyword: "laptop", CategoryID: 10, BudgetMin: "3000.00", BudgetMax: "5000.00", Limit: 5,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if product.searchRequest.Keyword != "laptop" || product.searchRequest.CategoryID != 10 || product.searchRequest.Limit != 5 {
		t.Fatalf("search request=%#v", product.searchRequest)
	}
	if result.ToolName != ToolSearchProducts || result.Output.(ProductSearchResult).Products[0].ProductID != 1001 {
		t.Fatalf("result=%#v", result)
	}
}

func TestGetPriceStockToolCallsCheckoutSKUs(t *testing.T) {
	product := &fakeProductClient{
		checkoutSKUs: []CheckoutSKU{{
			ProductID: 1001, SKUID: 2001, ProductTitle: "轻薄笔记本", SKUCode: "LAPTOP-16G",
			SpecJSON: []byte(`{"memory":"16G"}`), SalePrice: "4999.00", Saleable: true,
		}},
	}
	executor := NewToolExecutor(NewDefaultToolRegistry(time.Second), product, nil, nil)

	result, err := executor.Execute(context.Background(), ToolInvocation{
		Name: ToolGetPriceStock, UserID: 42, Args: GetPriceStockArgs{SKUIDs: []uint64{2001}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(product.checkoutIDs) != 1 || product.checkoutIDs[0] != 2001 {
		t.Fatalf("checkout sku ids=%v", product.checkoutIDs)
	}
	output := result.Output.(PriceStockResult)
	if len(output.SKUs) != 1 || !output.SKUs[0].Saleable || output.SKUs[0].SalePrice != "4999.00" {
		t.Fatalf("output=%#v", output)
	}
}

func TestGetDiscountToolUsesCheckoutPromotions(t *testing.T) {
	product := &fakeProductClient{
		checkoutSKUs: []CheckoutSKU{{
			ProductID: 1001, SKUID: 2001, ProductTitle: "轻薄笔记本", SalePrice: "4999.00", Saleable: true,
			Promotions: []PromotionSummary{{PromotionID: 3001, RuleType: "FULL_REDUCTION", ThresholdAmount: "5000.00", DiscountAmount: "300.00"}},
		}},
	}
	executor := NewToolExecutor(NewDefaultToolRegistry(time.Second), product, nil, nil)

	result, err := executor.Execute(context.Background(), ToolInvocation{
		Name: ToolGetDiscount, UserID: 42, Args: GetDiscountArgs{SKUIDs: []uint64{2001}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(product.checkoutIDs) != 1 || product.checkoutIDs[0] != 2001 {
		t.Fatalf("checkout sku ids=%v", product.checkoutIDs)
	}
	output := result.Output.(DiscountResult)
	if output.UserID != 42 || len(output.Items) != 1 || output.Items[0].Promotions[0].DiscountAmount != "300.00" {
		t.Fatalf("output=%#v", output)
	}
}

func TestGetUserProfileToolCallsUserService(t *testing.T) {
	user := &fakeUserClient{
		profile: UserProfile{
			UserID: 42, Email: "buyer@example.com", PreferenceJSON: []byte(`{"brand":"ThinkPad"}`),
			BudgetMin: "3000.00", BudgetMax: "6000.00", ProfileVersion: 7,
		},
	}
	executor := NewToolExecutor(NewDefaultToolRegistry(time.Second), nil, user, nil)

	result, err := executor.Execute(context.Background(), ToolInvocation{
		Name: ToolGetUserProfile, UserID: 42, Args: struct{}{},
	})
	if err != nil {
		t.Fatal(err)
	}
	output := result.Output.(UserProfile)
	if output.UserID != 42 || output.Email != "buyer@example.com" || output.ProfileVersion != 7 {
		t.Fatalf("output=%#v", output)
	}
}

func TestSearchProductKnowledgeToolReturnsControlledFallback(t *testing.T) {
	knowledge := &fakeKnowledgeClient{
		result: KnowledgeSearchResult{FallbackReason: "NO_READY_KNOWLEDGE"},
	}
	executor := NewToolExecutor(NewDefaultToolRegistry(time.Second), nil, nil, knowledge)

	result, err := executor.Execute(context.Background(), ToolInvocation{
		Name: ToolSearchProductKnowledge, UserID: 42,
		Args: SearchProductKnowledgeArgs{ProductID: 1001, Query: "battery warranty", DocTypes: []string{"FAQ"}, TopK: 5},
	})
	if err != nil {
		t.Fatal(err)
	}
	if knowledge.request.ProductID != 1001 || knowledge.request.Query != "battery warranty" || knowledge.request.TopK != 5 {
		t.Fatalf("knowledge request=%#v", knowledge.request)
	}
	output := result.Output.(KnowledgeSearchResult)
	if output.FallbackReason != "NO_READY_KNOWLEDGE" || len(output.Snippets) != 0 {
		t.Fatalf("output=%#v", output)
	}
}

func TestToolExecutorMapsDependencyTimeout(t *testing.T) {
	product := &fakeProductClient{searchErr: context.DeadlineExceeded}
	executor := NewToolExecutor(NewDefaultToolRegistry(time.Second), product, nil, nil)

	_, err := executor.Execute(context.Background(), ToolInvocation{
		Name: ToolSearchProducts, UserID: 42, Args: SearchProductsArgs{Keyword: "laptop", Limit: 5},
	})
	if !errors.Is(err, ErrDependencyTimeout) {
		t.Fatalf("Execute() error = %v, want ErrDependencyTimeout", err)
	}
}

type fakeProductClient struct {
	searchRequest ProductSearchRequest
	searchResult  ProductSearchResult
	searchErr     error
	checkoutIDs   []uint64
	checkoutSKUs  []CheckoutSKU
	checkoutErr   error
}

func (f *fakeProductClient) ListProducts(ctx context.Context, request ProductSearchRequest) (ProductSearchResult, error) {
	f.searchRequest = request
	return f.searchResult, f.searchErr
}

func (f *fakeProductClient) GetCheckoutSKUs(ctx context.Context, skuIDs []uint64) ([]CheckoutSKU, error) {
	f.checkoutIDs = append([]uint64(nil), skuIDs...)
	return f.checkoutSKUs, f.checkoutErr
}

type fakeUserClient struct {
	profile UserProfile
	err     error
}

func (f *fakeUserClient) GetMyProfile(ctx context.Context) (UserProfile, error) {
	return f.profile, f.err
}

type fakeKnowledgeClient struct {
	request KnowledgeSearchRequest
	result  KnowledgeSearchResult
	err     error
}

func (f *fakeKnowledgeClient) SearchProductKnowledge(ctx context.Context, request KnowledgeSearchRequest) (KnowledgeSearchResult, error) {
	f.request = request
	return f.result, f.err
}
