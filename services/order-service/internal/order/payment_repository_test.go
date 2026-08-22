package order

import (
	"context"
	"errors"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestMySQLPaymentRepositorySettleWritesLedgerAndOutboxInOneTransaction(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	attempt := PaymentAttempt{ID: "attempt-1", ReservationID: "reservation-1"}

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(queryPaymentOrderForUpdate)).WithArgs(uint64(7), "order-1").WillReturnRows(sqlmock.NewRows(paymentOrderColumns()).AddRow(uint64(11), "order-1", uint64(7), PaymentProcessing, "12.00", "0.00", attempt.ID, attempt.ReservationID))
	mock.ExpectQuery(regexp.QuoteMeta(queryWalletForUpdate)).WithArgs(uint64(7)).WillReturnRows(sqlmock.NewRows([]string{"balance", "version"}).AddRow("20.00", uint64(3)))
	mock.ExpectExec(regexp.QuoteMeta(updateWalletBalance)).WithArgs("8.00", uint64(7), uint64(3)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(insertWalletLedger)).WithArgs(uint64(7), "ORDER_PAYMENT", "order-1", "DEBIT", "12.00").WillReturnResult(sqlmock.NewResult(51, 1))
	mock.ExpectExec(regexp.QuoteMeta(updateOrderPaid)).WithArgs("12.00", uint64(11), attempt.ID, attempt.ReservationID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(insertReservationConfirmationOutbox)).WithArgs(attempt.ReservationID, "INVENTORY_RESERVATION", attempt.ReservationID, "CONFIRM", "inventory.reservation.confirm", attempt.ReservationID, `{"reservation_id":"reservation-1"}`).WillReturnResult(sqlmock.NewResult(61, 1))
	mock.ExpectCommit()
	mock.ExpectQuery(regexp.QuoteMeta(queryOrderItems)).WithArgs(uint64(11)).WillReturnRows(sqlmock.NewRows(orderItemColumns()).AddRow(uint64(31), uint64(11), uint64(101), uint64(201), "item", "sku", `{"size":"M"}`, `[]`, `null`, "12.00", "0.00", uint32(1), "12.00"))

	got, err := NewMySQLRepository(db).SettleWalletPayment(context.Background(), 7, "order-1", attempt)
	if err != nil || got.Status != Paid || got.PaidAmount != "12.00" || got.Payment != attempt || len(got.Items) != 1 {
		t.Fatalf("SettleWalletPayment() = %#v, %v", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMySQLPaymentRepositorySettlesInsufficientBalanceByRollingBack(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	attempt := PaymentAttempt{ID: "attempt-1", ReservationID: "reservation-1"}

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(queryPaymentOrderForUpdate)).WithArgs(uint64(7), "order-1").WillReturnRows(sqlmock.NewRows(paymentOrderColumns()).AddRow(uint64(11), "order-1", uint64(7), PaymentProcessing, "12.00", "0.00", attempt.ID, attempt.ReservationID))
	mock.ExpectQuery(regexp.QuoteMeta(queryWalletForUpdate)).WithArgs(uint64(7)).WillReturnRows(sqlmock.NewRows([]string{"balance", "version"}).AddRow("11.99", uint64(3)))
	mock.ExpectRollback()

	_, err = NewMySQLRepository(db).SettleWalletPayment(context.Background(), 7, "order-1", attempt)
	if !errors.Is(err, ErrInsufficientBalance) {
		t.Fatalf("SettleWalletPayment error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMySQLPaymentRepositoryClaimRejectsConcurrentProcessingPayment(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(queryPaymentOrderForUpdate)).WithArgs(uint64(7), "order-1").WillReturnRows(sqlmock.NewRows(paymentOrderColumns()).AddRow(uint64(11), "order-1", uint64(7), PaymentProcessing, "12.00", "0.00", "attempt-1", "reservation-1"))
	mock.ExpectRollback()

	_, err = NewMySQLRepository(db).ClaimPayment(context.Background(), 7, "order-1", PaymentAttempt{ID: "attempt-2", ReservationID: "reservation-2"})
	if !errors.Is(err, ErrPaymentInProgress) {
		t.Fatalf("ClaimPayment error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMySQLPaymentRepositoryRollsBackWhenLedgerOrOutboxWriteFails(t *testing.T) {
	cases := []struct {
		name      string
		failureAt string
	}{
		{name: "duplicate ledger", failureAt: "ledger"},
		{name: "outbox insert", failureAt: "outbox"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			attempt := PaymentAttempt{ID: "attempt-1", ReservationID: "reservation-1"}
			mock.ExpectBegin()
			mock.ExpectQuery(regexp.QuoteMeta(queryPaymentOrderForUpdate)).WithArgs(uint64(7), "order-1").WillReturnRows(sqlmock.NewRows(paymentOrderColumns()).AddRow(uint64(11), "order-1", uint64(7), PaymentProcessing, "12.00", "0.00", attempt.ID, attempt.ReservationID))
			mock.ExpectQuery(regexp.QuoteMeta(queryWalletForUpdate)).WithArgs(uint64(7)).WillReturnRows(sqlmock.NewRows([]string{"balance", "version"}).AddRow("20.00", uint64(3)))
			mock.ExpectExec(regexp.QuoteMeta(updateWalletBalance)).WithArgs("8.00", uint64(7), uint64(3)).WillReturnResult(sqlmock.NewResult(0, 1))
			if tc.failureAt == "ledger" {
				mock.ExpectExec(regexp.QuoteMeta(insertWalletLedger)).WithArgs(uint64(7), "ORDER_PAYMENT", "order-1", "DEBIT", "12.00").WillReturnError(errors.New("duplicate ledger"))
			} else {
				mock.ExpectExec(regexp.QuoteMeta(insertWalletLedger)).WithArgs(uint64(7), "ORDER_PAYMENT", "order-1", "DEBIT", "12.00").WillReturnResult(sqlmock.NewResult(51, 1))
				mock.ExpectExec(regexp.QuoteMeta(updateOrderPaid)).WithArgs("12.00", uint64(11), attempt.ID, attempt.ReservationID).WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectExec(regexp.QuoteMeta(insertReservationConfirmationOutbox)).WithArgs(attempt.ReservationID, "INVENTORY_RESERVATION", attempt.ReservationID, "CONFIRM", "inventory.reservation.confirm", attempt.ReservationID, `{"reservation_id":"reservation-1"}`).WillReturnError(errors.New("outbox unavailable"))
			}
			mock.ExpectRollback()

			if _, err := NewMySQLRepository(db).SettleWalletPayment(context.Background(), 7, "order-1", attempt); err == nil {
				t.Fatal("SettleWalletPayment error = nil")
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func paymentOrderColumns() []string {
	return []string{"id", "order_no", "user_id", "status", "total_amount", "paid_amount", "payment_attempt_id", "reservation_id"}
}

func orderItemColumns() []string {
	return []string{"id", "order_id", "product_id", "sku_id", "product_title_snapshot", "sku_code_snapshot", "sku_spec_snapshot", "candidate_promotions_snapshot", "promotion_snapshot", "unit_price", "discount_amount", "quantity", "item_amount"}
}
