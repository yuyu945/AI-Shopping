package order

import (
	"context"
	"database/sql/driver"
	"errors"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/go-sql-driver/mysql"
)

func TestMySQLRepositoryGetCartReturnsStableEmptyCart(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectQuery(regexp.QuoteMeta(queryCart)).WithArgs(uint64(7)).WillReturnRows(sqlmock.NewRows([]string{"id", "user_id"}))
	repository := NewMySQLRepository(db)

	cart, err := repository.GetCart(context.Background(), 7)
	if err != nil {
		t.Fatalf("GetCart: %v", err)
	}
	if cart.UserID != 7 || cart.ID != 0 || len(cart.Items) != 0 {
		t.Fatalf("cart = %#v, want stable empty cart for owner", cart)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMySQLRepositoryCartMutationsAreOwnerScoped(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*MySQLRepository) error
		expect func(sqlmock.Sqlmock)
	}{
		{
			name: "update foreign item", mutate: func(r *MySQLRepository) error {
				return r.UpdateCartItem(context.Background(), 8, 9, UpdateCartItemInput{Quantity: 2, Selected: true})
			}, expect: func(mock sqlmock.Sqlmock) {
				mock.ExpectExec(regexp.QuoteMeta(updateCartItem)).WithArgs(uint32(2), true, uint64(9), uint64(8)).WillReturnResult(sqlmock.NewResult(0, 0))
			},
		},
		{
			name: "delete foreign item", mutate: func(r *MySQLRepository) error {
				return r.DeleteCartItem(context.Background(), 8, 9)
			}, expect: func(mock sqlmock.Sqlmock) {
				mock.ExpectExec(regexp.QuoteMeta(deleteCartItem)).WithArgs(uint64(9), uint64(8)).WillReturnResult(sqlmock.NewResult(0, 0))
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			tc.expect(mock)

			err = tc.mutate(NewMySQLRepository(db))
			if !errors.Is(err, ErrNotFound) {
				t.Fatalf("mutation error = %v, want ErrNotFound", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestMySQLRepositoryCreateOrderRollsBackItemFailure(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	order := persistedOrder()

	mock.ExpectQuery(regexp.QuoteMeta(queryOrderByRequest)).WithArgs(order.UserID, order.RequestID).WillReturnRows(sqlmock.NewRows(orderColumns()))
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(insertOrder)).WithArgs(orderInsertArgs(order)...).WillReturnResult(sqlmock.NewResult(41, 1))
	mock.ExpectExec(regexp.QuoteMeta(insertOrderItem)).WithArgs(orderItemInsertArgs(41, order.Items[0])...).WillReturnError(errors.New("item insert failed"))
	mock.ExpectRollback()

	_, err = NewMySQLRepository(db).CreateOrder(context.Background(), order)
	if err == nil {
		t.Fatal("CreateOrder error = nil, want rollback error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMySQLRepositoryCreateOrderPersistsSnapshots(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	order := persistedOrder()

	mock.ExpectQuery(regexp.QuoteMeta(queryOrderByRequest)).WithArgs(order.UserID, order.RequestID).WillReturnRows(sqlmock.NewRows(orderColumns()))
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(insertOrder)).WithArgs(orderInsertArgs(order)...).WillReturnResult(sqlmock.NewResult(41, 1))
	mock.ExpectExec(regexp.QuoteMeta(insertOrderItem)).WithArgs(orderItemInsertArgs(41, order.Items[0])...).WillReturnResult(sqlmock.NewResult(61, 1))
	mock.ExpectCommit()

	created, err := NewMySQLRepository(db).CreateOrder(context.Background(), order)
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}
	if created.ID != 41 || created.Items[0].ID != 61 || created.Items[0].AppliedPromotion == nil || created.Items[0].AppliedPromotion.PromotionID != 5 {
		t.Fatalf("created = %#v, want persisted snapshots", created)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMySQLRepositoryCreateOrderReplaysExistingRequest(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	order := persistedOrder()
	expectStoredOrder(mock, order)

	replayed, err := NewMySQLRepository(db).CreateOrder(context.Background(), order)
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}
	if replayed.OrderNo != order.OrderNo || replayed.Items[0].ProductTitleSnapshot != "Keyboard" {
		t.Fatalf("replayed = %#v, want stored order", replayed)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMySQLRepositoryCreateOrderReReadsAfterDuplicateRace(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	order := persistedOrder()
	mock.ExpectQuery(regexp.QuoteMeta(queryOrderByRequest)).WithArgs(order.UserID, order.RequestID).WillReturnRows(sqlmock.NewRows(orderColumns()))
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(insertOrder)).WithArgs(orderInsertArgs(order)...).WillReturnError(&mysql.MySQLError{Number: 1062, Message: "duplicate key"})
	mock.ExpectRollback()
	expectStoredOrder(mock, order)

	replayed, err := NewMySQLRepository(db).CreateOrder(context.Background(), order)
	if err != nil || replayed.OrderNo != order.OrderNo {
		t.Fatalf("CreateOrder = %#v, %v; want stored order", replayed, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func persistedOrder() Order {
	return Order{ID: 40, OrderNo: "ORD-1", RequestID: "request-1", UserID: 7, Status: PendingPayment, TotalAmount: "178.00", PaidAmount: "0.00", Shipping: AddressSnapshot{AddressID: 11, ReceiverName: "Ada", ReceiverPhone: "13800138000", Province: "Zhejiang", City: "Hangzhou", District: "Xihu", Detail: "No. 1"}, Items: []OrderItem{{ID: 60, ProductID: 21, SKUID: 101, ProductTitleSnapshot: "Keyboard", SKUCodeSnapshot: "KB-1", SpecSnapshot: []byte(`{"color":"black"}`), UnitPrice: "99.00", DiscountAmount: "20.00", Quantity: 2, ItemAmount: "178.00", AppliedPromotion: &PromotionSnapshot{PromotionID: 5, RuleType: "DIRECT", DiscountAmount: "10.00"}}}}
}

func orderColumns() []string {
	return []string{"id", "order_no", "request_id", "user_id", "status", "total_amount", "paid_amount", "shipping_name_snapshot", "shipping_phone_snapshot", "shipping_address_snapshot"}
}

func orderInsertArgs(order Order) []driver.Value {
	return []driver.Value{order.OrderNo, order.UserID, order.RequestID, string(order.Status), order.TotalAmount, order.PaidAmount, order.Shipping.ReceiverName, order.Shipping.ReceiverPhone, sqlmock.AnyArg()}
}

func orderItemInsertArgs(orderID uint64, item OrderItem) []driver.Value {
	return []driver.Value{orderID, item.ProductID, item.SKUID, item.ProductTitleSnapshot, item.SKUCodeSnapshot, item.SpecSnapshot, sqlmock.AnyArg(), item.UnitPrice, item.DiscountAmount, item.Quantity, item.ItemAmount}
}

func expectStoredOrder(mock sqlmock.Sqlmock, order Order) {
	mock.ExpectQuery(regexp.QuoteMeta(queryOrderByRequest)).WithArgs(order.UserID, order.RequestID).WillReturnRows(sqlmock.NewRows(orderColumns()).AddRow(order.ID, order.OrderNo, order.RequestID, order.UserID, order.Status, order.TotalAmount, order.PaidAmount, order.Shipping.ReceiverName, order.Shipping.ReceiverPhone, `{"province":"Zhejiang","city":"Hangzhou","district":"Xihu","detail":"No. 1"}`))
	mock.ExpectQuery(regexp.QuoteMeta(queryOrderItems)).WithArgs(order.ID).WillReturnRows(sqlmock.NewRows([]string{"id", "order_id", "product_id", "sku_id", "product_title_snapshot", "sku_code_snapshot", "sku_spec_snapshot", "promotion_snapshot", "unit_price", "discount_amount", "quantity", "item_amount"}).AddRow(order.Items[0].ID, order.ID, order.Items[0].ProductID, order.Items[0].SKUID, order.Items[0].ProductTitleSnapshot, order.Items[0].SKUCodeSnapshot, order.Items[0].SpecSnapshot, `{"promotion_id":5,"rule_type":"DIRECT","discount_amount":"10.00"}`, order.Items[0].UnitPrice, order.Items[0].DiscountAmount, order.Items[0].Quantity, order.Items[0].ItemAmount))
}
