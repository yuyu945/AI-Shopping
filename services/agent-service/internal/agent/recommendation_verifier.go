package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// RecommendationVerifier verifies model recommendation candidates against backend snapshots.
type RecommendationVerifier struct {
	product ProductClient
	timeout time.Duration
}

// NewRecommendationVerifier constructs a backend recommendation verifier.
func NewRecommendationVerifier(product ProductClient, timeout time.Duration) *RecommendationVerifier {
	if timeout <= 0 {
		timeout = time.Second
	}
	return &RecommendationVerifier{product: product, timeout: timeout}
}

// Verify returns only backend-verified recommendation snapshots.
func (v *RecommendationVerifier) Verify(ctx context.Context, output FinalRecommendationOutput) ([]RecommendationSnapshot, error) {
	if v.product == nil {
		return nil, fmt.Errorf("%w: product client unavailable", ErrToolFailed)
	}
	snapshots := make([]RecommendationSnapshot, 0, len(output.Recommendations))
	for _, candidate := range output.Recommendations {
		callCtx, cancel := context.WithTimeout(ctx, v.timeout)
		skus, err := v.product.GetCheckoutSKUs(callCtx, []uint64{candidate.SKUID})
		timedOut := errors.Is(err, context.DeadlineExceeded) || errors.Is(callCtx.Err(), context.DeadlineExceeded)
		cancel()
		if timedOut {
			return nil, ErrDependencyTimeout
		}
		if errors.Is(err, ErrCheckoutSKUUnavailable) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("%w: verify recommendations", ErrToolFailed)
		}
		if len(skus) != 1 || skus[0].SKUID != candidate.SKUID || !skus[0].Saleable {
			continue
		}
		sku := skus[0]
		discountJSON, err := json.Marshal(sku.Promotions)
		if err != nil {
			return nil, fmt.Errorf("%w: encode discounts", ErrToolFailed)
		}
		snapshots = append(snapshots, RecommendationSnapshot{
			RankNo: candidate.RankNo, SKUID: candidate.SKUID, ProductID: sku.ProductID,
			ProductTitleSnapshot: sku.ProductTitle, SKUCodeSnapshot: sku.SKUCode,
			SKUSpecSnapshotJSON: append([]byte(nil), sku.SpecJSON...), PriceSnapshot: sku.SalePrice,
			SaleableSnapshot: sku.Saleable, DiscountSnapshotJSON: discountJSON,
			Reason: candidate.Reason, ValidationStatus: RecommendationVerified,
		})
	}
	if len(snapshots) == 0 {
		return nil, ErrNoValidRecommendation
	}
	return snapshots, nil
}
