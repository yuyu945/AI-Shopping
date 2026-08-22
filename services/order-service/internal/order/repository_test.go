package order

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"reflect"
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
	if created.ID != 41 || created.Items[0].ID != 61 || created.Items[0].AppliedPromotion == nil || created.Items[0].AppliedPromotion.PromotionID != 5 || len(created.Items[0].CandidatePromotions) != 2 {
		t.Fatalf("created = %#v, want persisted snapshots", created)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMySQLRepositoryCreateOrderRejectsUnpersistableMoney(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Order)
	}{
		{name: "order total exceeds decimal maximum", mutate: func(order *Order) { order.TotalAmount = "10000000000.00" }},
		{name: "item price has excess precision", mutate: func(order *Order) { order.Items[0].UnitPrice = "99.001" }},
		{name: "item discount exceeds decimal maximum", mutate: func(order *Order) { order.Items[0].DiscountAmount = "10000000000.00" }},
		{name: "item amount is negative", mutate: func(order *Order) { order.Items[0].ItemAmount = "-0.01" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			order := persistedOrder()
			tc.mutate(&order)
			mock.ExpectQuery(regexp.QuoteMeta(queryOrderByRequest)).WithArgs(order.UserID, order.RequestID).WillReturnRows(sqlmock.NewRows(orderColumns()))

			_, err = NewMySQLRepository(db).CreateOrder(context.Background(), order)
			if err == nil || err.Error() != "order amounts are invalid" {
				t.Fatalf("CreateOrder error = %v, want stable amount validation error", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestMySQLRepositoryPersistsCandidateSnapshotsWithoutAppliedPromotion(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	order := persistedOrder()
	order.Items[0].AppliedPromotion = nil

	mock.ExpectQuery(regexp.QuoteMeta(queryOrderByRequest)).WithArgs(order.UserID, order.RequestID).WillReturnRows(sqlmock.NewRows(orderColumns()))
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(insertOrder)).WithArgs(orderInsertArgs(order)...).WillReturnResult(sqlmock.NewResult(41, 1))
	mock.ExpectExec(regexp.QuoteMeta(insertOrderItem)).WithArgs(orderItemInsertArgs(41, order.Items[0])...).WillReturnResult(sqlmock.NewResult(61, 1))
	mock.ExpectCommit()

	created, err := NewMySQLRepository(db).CreateOrder(context.Background(), order)
	if err != nil || created.Items[0].AppliedPromotion != nil || len(created.Items[0].CandidatePromotions) != 2 {
		t.Fatalf("CreateOrder = %#v, %v", created, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMySQLRepositoryLoadsCandidatePromotionSnapshots(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	order := persistedOrder()
	order.Items[0].AppliedPromotion = nil

	expectOrderByNumber(mock, order)
	expectOrderByNumber(mock, order)

	loaded, err := NewMySQLRepository(db).GetOrder(context.Background(), order.UserID, order.OrderNo)
	if err != nil {
		t.Fatalf("GetOrder: %v", err)
	}
	item := loaded.Items[0]
	if item.AppliedPromotion != nil || len(item.CandidatePromotions) != 2 || item.CandidatePromotions[0].PromotionID != 5 || item.CandidatePromotions[1].PromotionID != 9 {
		t.Fatalf("loaded promotion snapshots = %#v", item)
	}
	item.CandidatePromotions[0].DiscountAmount = "0.00"
	loadedAgain, err := NewMySQLRepository(db).GetOrder(context.Background(), order.UserID, order.OrderNo)
	if err != nil || loadedAgain.Items[0].CandidatePromotions[0].DiscountAmount != "10.00" {
		t.Fatalf("second GetOrder reused mutable result: %#v, %v", loadedAgain, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMySQLRepositoryRejectsInvalidCandidatePromotionSnapshots(t *testing.T) {
	cases := []struct {
		name           string
		candidatesJSON string
		appliedJSON    string
	}{
		{name: "null candidates", candidatesJSON: `null`, appliedJSON: `null`},
		{name: "invalid candidates json", candidatesJSON: `{`, appliedJSON: `null`},
		{name: "applied promotion is not a candidate", candidatesJSON: `[{"promotion_id":9,"rule_type":"DIRECT","discount_amount":"5.00"}]`, appliedJSON: `{"promotion_id":5,"rule_type":"DIRECT","discount_amount":"10.00"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			order := persistedOrder()
			mock.ExpectQuery(regexp.QuoteMeta(queryOrderByNumber)).WithArgs(order.UserID, order.OrderNo).WillReturnRows(sqlmock.NewRows(orderColumns()).AddRow(order.ID, order.OrderNo, order.RequestID, order.UserID, order.Status, order.TotalAmount, order.PaidAmount, order.Shipping.ReceiverName, order.Shipping.ReceiverPhone, `{"province":"Zhejiang","city":"Hangzhou","district":"Xihu","detail":"No. 1"}`))
			mock.ExpectQuery(regexp.QuoteMeta(queryOrderItems)).WithArgs(order.ID).WillReturnRows(sqlmock.NewRows([]string{"id", "order_id", "product_id", "sku_id", "product_title_snapshot", "sku_code_snapshot", "sku_spec_snapshot", "candidate_promotions_snapshot", "promotion_snapshot", "unit_price", "discount_amount", "quantity", "item_amount"}).AddRow(order.Items[0].ID, order.ID, order.Items[0].ProductID, order.Items[0].SKUID, order.Items[0].ProductTitleSnapshot, order.Items[0].SKUCodeSnapshot, order.Items[0].SpecSnapshot, tc.candidatesJSON, tc.appliedJSON, order.Items[0].UnitPrice, order.Items[0].DiscountAmount, order.Items[0].Quantity, order.Items[0].ItemAmount))

			_, err = NewMySQLRepository(db).GetOrder(context.Background(), order.UserID, order.OrderNo)
			if err == nil {
				t.Fatal("GetOrder error = nil, want safe snapshot rejection")
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestMySQLRepositoryRejectsIncompleteShippingAddressSnapshots(t *testing.T) {
	cases := []struct {
		name          string
		shippingJSON  string
		receiverName  string
		receiverPhone string
	}{
		{name: "json null", shippingJSON: `null`, receiverName: "Ada", receiverPhone: "13800138000"},
		{name: "empty json object", shippingJSON: `{}`, receiverName: "Ada", receiverPhone: "13800138000"},
		{name: "missing district", shippingJSON: `{"province":"Zhejiang","city":"Hangzhou","detail":"No. 1"}`, receiverName: "Ada", receiverPhone: "13800138000"},
		{name: "missing receiver name", shippingJSON: `{"province":"Zhejiang","city":"Hangzhou","district":"Xihu","detail":"No. 1"}`, receiverPhone: "13800138000"},
		{name: "missing receiver phone", shippingJSON: `{"province":"Zhejiang","city":"Hangzhou","district":"Xihu","detail":"No. 1"}`, receiverName: "Ada"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			order := persistedOrder()
			mock.ExpectQuery(regexp.QuoteMeta(queryOrderByNumber)).WithArgs(order.UserID, order.OrderNo).WillReturnRows(sqlmock.NewRows(orderColumns()).AddRow(order.ID, order.OrderNo, order.RequestID, order.UserID, order.Status, order.TotalAmount, order.PaidAmount, tc.receiverName, tc.receiverPhone, tc.shippingJSON))

			_, err = NewMySQLRepository(db).GetOrder(context.Background(), order.UserID, order.OrderNo)
			if err == nil || err.Error() != "stored shipping address is invalid" {
				t.Fatalf("GetOrder error = %v, want stable shipping address error", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
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
	return Order{ID: 40, OrderNo: "ORD-1", RequestID: "request-1", UserID: 7, Status: PendingPayment, TotalAmount: "178.00", PaidAmount: "0.00", Shipping: AddressSnapshot{AddressID: 11, ReceiverName: "Ada", ReceiverPhone: "13800138000", Province: "Zhejiang", City: "Hangzhou", District: "Xihu", Detail: "No. 1"}, Items: []OrderItem{{ID: 60, ProductID: 21, SKUID: 101, ProductTitleSnapshot: "Keyboard", SKUCodeSnapshot: "KB-1", SpecSnapshot: []byte(`{"color":"black"}`), UnitPrice: "99.00", DiscountAmount: "20.00", Quantity: 2, ItemAmount: "178.00", CandidatePromotions: []PromotionSnapshot{{PromotionID: 5, RuleType: "DIRECT", DiscountAmount: "10.00"}, {PromotionID: 9, RuleType: "DIRECT", ThresholdAmount: "200.00", DiscountAmount: "5.00"}}, AppliedPromotion: &PromotionSnapshot{PromotionID: 5, RuleType: "DIRECT", DiscountAmount: "10.00"}}}}
}

func orderColumns() []string {
	return []string{"id", "order_no", "request_id", "user_id", "status", "total_amount", "paid_amount", "shipping_name_snapshot", "shipping_phone_snapshot", "shipping_address_snapshot"}
}

func orderInsertArgs(order Order) []driver.Value {
	return []driver.Value{order.OrderNo, order.UserID, order.RequestID, string(order.Status), order.TotalAmount, order.PaidAmount, order.Shipping.ReceiverName, order.Shipping.ReceiverPhone, sqlmock.AnyArg()}
}

func orderItemInsertArgs(orderID uint64, item OrderItem) []driver.Value {
	candidates, err := json.Marshal(item.CandidatePromotions)
	if err != nil {
		panic(err)
	}
	applied := []byte("null")
	if item.AppliedPromotion != nil {
		applied, err = json.Marshal(item.AppliedPromotion)
		if err != nil {
			panic(err)
		}
	}
	return []driver.Value{orderID, item.ProductID, item.SKUID, item.ProductTitleSnapshot, item.SKUCodeSnapshot, item.SpecSnapshot, jsonArgument{expected: string(candidates)}, jsonArgument{expected: string(applied)}, item.UnitPrice, item.DiscountAmount, item.Quantity, item.ItemAmount}
}

type jsonArgument struct{ expected string }

func (a jsonArgument) Match(value driver.Value) bool {
	actual, ok := value.([]byte)
	if !ok {
		return false
	}
	var expectedValue any
	var actualValue any
	return json.Unmarshal([]byte(a.expected), &expectedValue) == nil && json.Unmarshal(actual, &actualValue) == nil && reflect.DeepEqual(expectedValue, actualValue)
}

func expectStoredOrder(mock sqlmock.Sqlmock, order Order) {
	mock.ExpectQuery(regexp.QuoteMeta(queryOrderByRequest)).WithArgs(order.UserID, order.RequestID).WillReturnRows(sqlmock.NewRows(orderColumns()).AddRow(order.ID, order.OrderNo, order.RequestID, order.UserID, order.Status, order.TotalAmount, order.PaidAmount, order.Shipping.ReceiverName, order.Shipping.ReceiverPhone, `{"province":"Zhejiang","city":"Hangzhou","district":"Xihu","detail":"No. 1"}`))
	mock.ExpectQuery(regexp.QuoteMeta(queryOrderItems)).WithArgs(order.ID).WillReturnRows(orderItemRows(order))
}

func expectOrderByNumber(mock sqlmock.Sqlmock, order Order) {
	mock.ExpectQuery(regexp.QuoteMeta(queryOrderByNumber)).WithArgs(order.UserID, order.OrderNo).WillReturnRows(sqlmock.NewRows(orderColumns()).AddRow(order.ID, order.OrderNo, order.RequestID, order.UserID, order.Status, order.TotalAmount, order.PaidAmount, order.Shipping.ReceiverName, order.Shipping.ReceiverPhone, `{"province":"Zhejiang","city":"Hangzhou","district":"Xihu","detail":"No. 1"}`))
	mock.ExpectQuery(regexp.QuoteMeta(queryOrderItems)).WithArgs(order.ID).WillReturnRows(orderItemRows(order))
}

func orderItemRows(order Order) *sqlmock.Rows {
	item := order.Items[0]
	appliedJSON := `{"promotion_id":5,"rule_type":"DIRECT","discount_amount":"10.00"}`
	if item.AppliedPromotion == nil {
		appliedJSON = `null`
	}
	return sqlmock.NewRows([]string{"id", "order_id", "product_id", "sku_id", "product_title_snapshot", "sku_code_snapshot", "sku_spec_snapshot", "candidate_promotions_snapshot", "promotion_snapshot", "unit_price", "discount_amount", "quantity", "item_amount"}).AddRow(item.ID, order.ID, item.ProductID, item.SKUID, item.ProductTitleSnapshot, item.SKUCodeSnapshot, item.SpecSnapshot, `[{"promotion_id":5,"rule_type":"DIRECT","discount_amount":"10.00"},{"promotion_id":9,"rule_type":"DIRECT","threshold_amount":"200.00","discount_amount":"5.00"}]`, appliedJSON, item.UnitPrice, item.DiscountAmount, item.Quantity, item.ItemAmount)
}
