package client

import (
	"context"
	"github.com/yuyu945/AI-Shopping/services/order-service/internal/order"
	productpb "github.com/yuyu945/AI-Shopping/services/product-service/gen"
	"google.golang.org/grpc"
	"time"
)

type ProductClient struct {
	client  productpb.ProductServiceClient
	timeout time.Duration
}

func NewProductClient(conn grpc.ClientConnInterface, timeout time.Duration) *ProductClient {
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	return &ProductClient{productpb.NewProductServiceClient(conn), timeout}
}
func (c *ProductClient) GetProducts(ctx context.Context, ids []uint64) ([]order.ProductSnapshot, error) {
	call, cancel := context.WithTimeout(outgoing(ctx), c.timeout)
	defer cancel()
	out, e := c.client.GetCheckoutSKUs(call, &productpb.CheckoutSKUsRequest{SkuIds: ids})
	if e != nil {
		return nil, dependencyError(e)
	}
	items := make([]order.ProductSnapshot, 0, len(out.GetSkus()))
	for _, v := range out.GetSkus() {
		item := order.ProductSnapshot{ProductID: v.GetProductId(), SKUID: v.GetSkuId(), ProductTitle: v.GetProductTitle(), SKUCode: v.GetSkuCode(), SpecJSON: append([]byte(nil), v.GetSpecJson()...), UnitPrice: v.GetSalePrice(), Saleable: v.GetSaleable()}
		for _, p := range v.GetPromotions() {
			item.Promotions = append(item.Promotions, order.PromotionSnapshot{PromotionID: p.GetPromotionId(), RuleType: p.GetRuleType(), ThresholdAmount: p.GetThresholdAmount(), DiscountAmount: p.GetDiscountAmount()})
		}
		items = append(items, item)
	}
	return items, nil
}
