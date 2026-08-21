package catalog

import "testing"

func TestPromotionSummaryPreservesNullableAmounts(t *testing.T) {
	promotion := PromotionSummary{ThresholdAmount: nil, DiscountAmount: nil}
	if promotion.ThresholdAmount != nil || promotion.DiscountAmount != nil {
		t.Fatalf("expected nullable amounts to remain nil: %#v", promotion)
	}
}
