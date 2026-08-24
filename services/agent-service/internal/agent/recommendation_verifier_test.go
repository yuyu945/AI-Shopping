package agent

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRecommendationVerifierBuildsBackendSnapshots(t *testing.T) {
	product := &fakeRecommendationProductClient{skus: []CheckoutSKU{{
		ProductID: 1001, SKUID: 2001, ProductTitle: "轻薄笔记本", SKUCode: "LAPTOP-16G",
		SpecJSON: []byte(`{"memory":"16G"}`), SalePrice: "4999.00", Saleable: true,
		Promotions: []PromotionSummary{{PromotionID: 3001, RuleType: "FULL_REDUCTION", DiscountAmount: "300.00"}},
	}}}
	verifier := NewRecommendationVerifier(product, time.Second)
	got, err := verifier.Verify(context.Background(), FinalRecommendationOutput{Recommendations: []ModelRecommendation{{SKUID: 2001, RankNo: 1, Reason: "模型声称价格 0.01 也会被忽略"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].PriceSnapshot != "4999.00" || got[0].ProductTitleSnapshot != "轻薄笔记本" {
		t.Fatalf("snapshots=%#v", got)
	}
	if string(got[0].DiscountSnapshotJSON) == "" || got[0].ValidationStatus != RecommendationVerified {
		t.Fatalf("snapshot=%#v", got[0])
	}
}

func TestRecommendationVerifierFiltersMissingAndUnsaleableSKUs(t *testing.T) {
	product := &fakeRecommendationProductClient{skus: []CheckoutSKU{{ProductID: 1001, SKUID: 2001, Saleable: false, SalePrice: "4999.00"}}}
	verifier := NewRecommendationVerifier(product, time.Second)
	_, err := verifier.Verify(context.Background(), FinalRecommendationOutput{Recommendations: []ModelRecommendation{
		{SKUID: 2001, RankNo: 1, Reason: "不可售"},
		{SKUID: 9999, RankNo: 2, Reason: "不存在"},
	}})
	if !errors.Is(err, ErrNoValidRecommendation) {
		t.Fatalf("error=%v, want ErrNoValidRecommendation", err)
	}
}

func TestRecommendationVerifierKeepsValidSKUWhenAnotherCandidateIsUnavailable(t *testing.T) {
	product := &fakeRecommendationProductClient{bySKU: map[uint64]CheckoutSKU{
		2001: {ProductID: 1001, SKUID: 2001, SalePrice: "4999.00", Saleable: true},
	}, unavailable: map[uint64]struct{}{9999: {}}}
	verifier := NewRecommendationVerifier(product, time.Second)
	got, err := verifier.Verify(context.Background(), FinalRecommendationOutput{Recommendations: []ModelRecommendation{
		{SKUID: 9999, RankNo: 1, Reason: "不存在"},
		{SKUID: 2001, RankNo: 2, Reason: "有效"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].SKUID != 2001 || got[0].RankNo != 2 {
		t.Fatalf("snapshots=%#v", got)
	}
}

func TestRecommendationVerifierUsesChangedBackendPromotions(t *testing.T) {
	product := &fakeRecommendationProductClient{skus: []CheckoutSKU{{
		ProductID: 1001, SKUID: 2001, SalePrice: "4999.00", Saleable: true,
		Promotions: []PromotionSummary{{PromotionID: 3002, RuleType: "FLASH", DiscountAmount: "500.00"}},
	}}}
	verifier := NewRecommendationVerifier(product, time.Second)
	got, err := verifier.Verify(context.Background(), FinalRecommendationOutput{Recommendations: []ModelRecommendation{{SKUID: 2001, RankNo: 1, Reason: "优惠以后台为准"}}})
	if err != nil {
		t.Fatal(err)
	}
	if string(got[0].DiscountSnapshotJSON) != `[{"promotion_id":3002,"rule_type":"FLASH","discount_amount":"500.00"}]` {
		t.Fatalf("discount snapshot=%s", got[0].DiscountSnapshotJSON)
	}
}

func TestRecommendationVerifierMapsDependencyTimeout(t *testing.T) {
	product := &fakeRecommendationProductClient{err: context.DeadlineExceeded}
	verifier := NewRecommendationVerifier(product, time.Second)
	_, err := verifier.Verify(context.Background(), FinalRecommendationOutput{Recommendations: []ModelRecommendation{{SKUID: 2001, RankNo: 1, Reason: "ok"}}})
	if !errors.Is(err, ErrDependencyTimeout) {
		t.Fatalf("error=%v, want ErrDependencyTimeout", err)
	}
}

type fakeRecommendationProductClient struct {
	request     []uint64
	skus        []CheckoutSKU
	bySKU       map[uint64]CheckoutSKU
	unavailable map[uint64]struct{}
	err         error
}

func (f *fakeRecommendationProductClient) ListProducts(context.Context, ProductSearchRequest) (ProductSearchResult, error) {
	return ProductSearchResult{}, nil
}

func (f *fakeRecommendationProductClient) GetCheckoutSKUs(ctx context.Context, skuIDs []uint64) ([]CheckoutSKU, error) {
	f.request = append([]uint64(nil), skuIDs...)
	if len(f.bySKU) > 0 || len(f.unavailable) > 0 {
		out := make([]CheckoutSKU, 0, len(skuIDs))
		for _, skuID := range skuIDs {
			if _, ok := f.unavailable[skuID]; ok {
				return nil, ErrCheckoutSKUUnavailable
			}
			sku, ok := f.bySKU[skuID]
			if !ok {
				return nil, ErrCheckoutSKUUnavailable
			}
			out = append(out, sku)
		}
		return out, f.err
	}
	return f.skus, f.err
}
