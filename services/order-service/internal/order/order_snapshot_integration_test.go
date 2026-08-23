//go:build integration

package order

import (
	"context"
	"os"
	"testing"
)

func TestOrderSnapshotMySQLIntegration(t *testing.T) {
	config, run, err := orderSnapshotIntegrationConfig(os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	if !run {
		t.Skip("set AI_SHOPPING_INTEGRATION=1 and AI_SHOPPING_INTEGRATION_ISOLATED=m21ordersnapshot to run the isolated order snapshot integration test")
	}

	fixture := newOrderSnapshotIntegrationFixture(t, context.Background(), config)
	t.Cleanup(fixture.cleanup)

	service := NewService(fixture.tradeRepository, fixture.addresses, fixture.products)
	item, err := service.AddCartItem(fixture.ctx, fixture.ownerID, AddCartItemInput{SKUID: fixture.skuID, Quantity: 2, Selected: true})
	if err != nil {
		t.Fatalf("add cart item: %v", err)
	}
	if err := service.UpdateCartItem(fixture.ctx, fixture.ownerID, item.ID, UpdateCartItemInput{Quantity: 3, Selected: true}); err != nil {
		t.Fatalf("update cart item: %v", err)
	}
	transient, err := service.AddCartItem(fixture.ctx, fixture.ownerID, AddCartItemInput{SKUID: fixture.secondarySKUID, Quantity: 1, Selected: false})
	if err != nil {
		t.Fatalf("add cart item to delete: %v", err)
	}
	if err := service.DeleteCartItem(fixture.ctx, fixture.ownerID, transient.ID); err != nil {
		t.Fatalf("delete cart item: %v", err)
	}
	cart, err := service.GetCart(fixture.ctx, fixture.ownerID)
	if err != nil {
		t.Fatalf("get cart after mutations: %v", err)
	}
	if len(cart.Items) != 1 || cart.Items[0].ID != item.ID || cart.Items[0].Quantity != 3 {
		t.Fatalf("cart after add/update/delete = %#v, want only updated item", cart.Items)
	}

	_, err = service.CreateOrder(fixture.ctx, fixture.ownerID, CreateOrderInput{RequestID: fixture.requestID + "-foreign", AddressID: fixture.foreignAddressID})
	if !IsCode(err, NotFound) {
		t.Fatalf("foreign address error = %v, want stable NOT_FOUND", err)
	}

	created, err := service.CreateOrder(fixture.ctx, fixture.ownerID, CreateOrderInput{RequestID: fixture.requestID, AddressID: fixture.ownerAddressID})
	if err != nil {
		t.Fatalf("create order: %v", err)
	}
	if len(created.Items) != 1 || created.Items[0].Quantity != 3 || created.Items[0].ProductTitleSnapshot != fixture.productTitle || created.Items[0].UnitPrice != fixture.salePrice {
		t.Fatalf("created snapshot = %#v, want original product facts", created.Items)
	}

	if _, err := fixture.catalogDB.ExecContext(fixture.ctx, "UPDATE products SET title = ? WHERE id = ?", fixture.productTitle+" changed", fixture.productID); err != nil {
		t.Fatalf("mutate catalog title: %v", err)
	}
	if _, err := fixture.catalogDB.ExecContext(fixture.ctx, "UPDATE product_skus SET sale_price = ? WHERE id = ?", "99.99", fixture.skuID); err != nil {
		t.Fatalf("mutate catalog sku price: %v", err)
	}

	if _, err := fixture.tradeRepository.FindOrderByRequest(fixture.ctx, fixture.ownerID, fixture.requestID); err != nil {
		t.Fatalf("load created order for idempotent replay: %v", err)
	}
	replayed, err := service.CreateOrder(fixture.ctx, fixture.ownerID, CreateOrderInput{RequestID: fixture.requestID, AddressID: fixture.ownerAddressID})
	if err != nil {
		t.Fatalf("replay create order: %v", err)
	}
	if replayed.ID != created.ID || replayed.OrderNo != created.OrderNo || len(replayed.Items) != 1 || replayed.Items[0].ProductTitleSnapshot != fixture.productTitle || replayed.Items[0].UnitPrice != fixture.salePrice {
		t.Fatalf("replayed snapshot = %#v, want original order %#v", replayed, created)
	}
	persisted, err := service.GetOrder(fixture.ctx, fixture.ownerID, created.OrderNo)
	if err != nil {
		t.Fatalf("get persisted order: %v", err)
	}
	if len(persisted.Items) != 1 || persisted.Items[0].ProductTitleSnapshot != fixture.productTitle || persisted.Items[0].UnitPrice != fixture.salePrice {
		t.Fatalf("persisted snapshot = %#v, want original catalog facts", persisted.Items)
	}
}
