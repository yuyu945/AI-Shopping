package catalog

import "testing"

func TestPromotionSummaryPreservesNullableThreshold(t *testing.T) {
	promotion := PromotionSummary{ThresholdAmount: nil, DiscountAmount: "300.00"}
	if promotion.ThresholdAmount != nil {
		t.Fatalf("expected nullable threshold to remain nil, got %v", *promotion.ThresholdAmount)
	}
}
