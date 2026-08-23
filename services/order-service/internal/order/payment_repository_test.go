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
	order := persistedOrder()
	order.ID, order.OrderNo, order.Status, order.TotalAmount, order.PaidAmount = 11, "order-1", Paid, "12.00", "12.00"

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(queryPaymentOrderForUpdate)).WithArgs(uint64(7), "order-1").WillReturnRows(sqlmock.NewRows(paymentOrderColumns()).AddRow(uint64(11), "order-1", uint64(7), PaymentProcessing, "12.00", "0.00", attempt.ID, attempt.ReservationID))
	mock.ExpectQuery(regexp.QuoteMeta(queryWalletForUpdate)).WithArgs(uint64(7)).WillReturnRows(sqlmock.NewRows([]string{"balance", "version"}).AddRow("20.00", uint64(3)))
	mock.ExpectExec(regexp.QuoteMeta(updateWalletBalance)).WithArgs("8.00", uint64(7), uint64(3)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(insertWalletLedger)).WithArgs(uint64(7), "ORDER_PAYMENT", "order-1", "DEBIT", "12.00").WillReturnResult(sqlmock.NewResult(51, 1))
	mock.ExpectExec(regexp.QuoteMeta(updateOrderPaid)).WithArgs("12.00", uint64(11), attempt.ID, attempt.ReservationID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(insertReservationConfirmationOutbox)).WithArgs(sqlmock.AnyArg(), "INVENTORY_RESERVATION", attempt.ReservationID, "CONFIRM", "inventory.reservation.confirm", attempt.ReservationID, `{"event_id":"reservation-1","reservation_id":"reservation-1","order_no":"order-1","payment_attempt_id":"attempt-1","version":1}`).WillReturnResult(sqlmock.NewResult(61, 1))
	mock.ExpectCommit()
	expectOrderByNumber(mock, order)

	got, err := NewMySQLRepository(db).SettleWalletPayment(context.Background(), 7, "order-1", attempt)
	if err != nil || got.Status != Paid || got.PaidAmount != "12.00" || got.Payment != attempt || got.RequestID != order.RequestID || got.Shipping.ReceiverName != order.Shipping.ReceiverName || got.Shipping.ReceiverPhone != order.Shipping.ReceiverPhone || got.Shipping.Detail != order.Shipping.Detail || len(got.Items) != 1 || got.Items[0].AppliedPromotion == nil || len(got.Items[0].CandidatePromotions) != 2 {
		t.Fatalf("SettleWalletPayment() = %#v, %v", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMySQLPaymentRepositorySettlesFullyDiscountedOrderWithoutWalletDebit(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	attempt := PaymentAttempt{ID: "attempt-1", ReservationID: "reservation-1"}
	order := persistedOrder()
	order.ID, order.OrderNo, order.Status, order.TotalAmount, order.PaidAmount = 11, "order-1", Paid, "0.00", "0.00"

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(queryPaymentOrderForUpdate)).WithArgs(uint64(7), "order-1").WillReturnRows(sqlmock.NewRows(paymentOrderColumns()).AddRow(uint64(11), "order-1", uint64(7), PaymentProcessing, "0.00", "0.00", attempt.ID, attempt.ReservationID))
	mock.ExpectExec(regexp.QuoteMeta(updateOrderPaid)).WithArgs("0.00", uint64(11), attempt.ID, attempt.ReservationID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(insertReservationConfirmationOutbox)).WithArgs(sqlmock.AnyArg(), "INVENTORY_RESERVATION", attempt.ReservationID, "CONFIRM", "inventory.reservation.confirm", attempt.ReservationID, `{"event_id":"reservation-1","reservation_id":"reservation-1","order_no":"order-1","payment_attempt_id":"attempt-1","version":1}`).WillReturnResult(sqlmock.NewResult(61, 1))
	mock.ExpectCommit()
	expectOrderByNumber(mock, order)

	got, err := NewMySQLRepository(db).SettleWalletPayment(context.Background(), 7, "order-1", attempt)
	if err != nil || got.Status != Paid || got.PaidAmount != "0.00" || got.Payment != attempt || got.RequestID != order.RequestID || got.Shipping.ReceiverName != order.Shipping.ReceiverName || got.Shipping.ReceiverPhone != order.Shipping.ReceiverPhone || got.Shipping.Detail != order.Shipping.Detail || len(got.Items) != 1 || got.Items[0].AppliedPromotion == nil || len(got.Items[0].CandidatePromotions) != 2 {
		t.Fatalf("SettleWalletPayment() = %#v, %v", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMySQLPaymentRepositoryRollsBackFullyDiscountedOrderWhenOutboxWriteFails(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	attempt := PaymentAttempt{ID: "attempt-1", ReservationID: "reservation-1"}

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(queryPaymentOrderForUpdate)).WithArgs(uint64(7), "order-1").WillReturnRows(sqlmock.NewRows(paymentOrderColumns()).AddRow(uint64(11), "order-1", uint64(7), PaymentProcessing, "0.00", "0.00", attempt.ID, attempt.ReservationID))
	mock.ExpectExec(regexp.QuoteMeta(updateOrderPaid)).WithArgs("0.00", uint64(11), attempt.ID, attempt.ReservationID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(insertReservationConfirmationOutbox)).WithArgs(sqlmock.AnyArg(), "INVENTORY_RESERVATION", attempt.ReservationID, "CONFIRM", "inventory.reservation.confirm", attempt.ReservationID, `{"event_id":"reservation-1","reservation_id":"reservation-1","order_no":"order-1","payment_attempt_id":"attempt-1","version":1}`).WillReturnError(errors.New("outbox unavailable"))
	mock.ExpectRollback()

	if _, err := NewMySQLRepository(db).SettleWalletPayment(context.Background(), 7, "order-1", attempt); err == nil {
		t.Fatal("SettleWalletPayment error = nil")
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

func TestMySQLPaymentRepositoryClaimTransitionsPendingPayment(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	attempt := PaymentAttempt{ID: "attempt-1", ReservationID: "reservation-1"}

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(queryPaymentOrderForUpdate)).WithArgs(uint64(7), "order-1").WillReturnRows(
		sqlmock.NewRows(paymentOrderColumns()).AddRow(uint64(11), "order-1", uint64(7), PendingPayment, "12.00", "0.00", nil, nil),
	)
	mock.ExpectExec(regexp.QuoteMeta(claimPaymentOrder)).WithArgs(PaymentProcessing, attempt.ID, attempt.ReservationID, uint64(11), uint64(7), PendingPayment).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	mock.ExpectQuery(regexp.QuoteMeta(queryOrderItems)).WithArgs(uint64(11)).WillReturnRows(sqlmock.NewRows(orderItemColumns()))

	got, err := NewMySQLRepository(db).ClaimPayment(context.Background(), 7, "order-1", attempt)
	if err != nil || got.Status != PaymentProcessing || got.Payment != attempt {
		t.Fatalf("ClaimPayment() = %#v, %v", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMySQLPaymentRepositoryClaimReturnsPaidOrderForReplay(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	paid := PaymentAttempt{ID: "attempt-1", ReservationID: "reservation-1"}
	order := persistedOrder()
	order.ID, order.OrderNo, order.Status, order.TotalAmount, order.PaidAmount = 11, "order-1", Paid, "12.00", "12.00"

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(queryPaymentOrderForUpdate)).WithArgs(uint64(7), "order-1").WillReturnRows(
		sqlmock.NewRows(paymentOrderColumns()).AddRow(uint64(11), "order-1", uint64(7), Paid, "12.00", "12.00", paid.ID, paid.ReservationID),
	)
	mock.ExpectCommit()
	expectOrderByNumber(mock, order)

	got, err := NewMySQLRepository(db).ClaimPayment(context.Background(), 7, "order-1", PaymentAttempt{ID: "attempt-2", ReservationID: "reservation-2"})
	if err != nil || got.Status != Paid || got.Payment != paid || len(got.Items) != 1 || got.Items[0].AppliedPromotion == nil || len(got.Items[0].CandidatePromotions) != 2 || got.Shipping.Detail != order.Shipping.Detail {
		t.Fatalf("ClaimPayment() = %#v, %v", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMySQLPaymentRepositoryClaimRejectsNonPayableOrderStates(t *testing.T) {
	for _, orderStatus := range []OrderStatus{"CLOSED", "UNKNOWN"} {
		t.Run(string(orderStatus), func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()

			mock.ExpectBegin()
			mock.ExpectQuery(regexp.QuoteMeta(queryPaymentOrderForUpdate)).WithArgs(uint64(7), "order-1").WillReturnRows(
				sqlmock.NewRows(paymentOrderColumns()).AddRow(uint64(11), "order-1", uint64(7), orderStatus, "12.00", "0.00", nil, nil),
			)
			mock.ExpectRollback()

			_, err = NewMySQLRepository(db).ClaimPayment(context.Background(), 7, "order-1", PaymentAttempt{ID: "attempt-1", ReservationID: "reservation-1"})
			if !IsCode(err, IdempotencyConflict) {
				t.Fatalf("ClaimPayment error = %v, want %s", err, IdempotencyConflict)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestMySQLPaymentRepositoryResetPaymentClaimReportsWhetherExactClaimChanged(t *testing.T) {
	cases := []struct {
		name    string
		rows    int64
		err     error
		reset   bool
		wantErr bool
	}{
		{name: "exact claim reset", rows: 1, reset: true},
		{name: "claim was settled concurrently", rows: 0, reset: false},
		{name: "database failure", err: errors.New("database unavailable"), wantErr: true},
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
			expect := mock.ExpectExec(regexp.QuoteMeta(resetPaymentOrder)).WithArgs(PendingPayment, uint64(7), "order-1", PaymentProcessing, attempt.ID, attempt.ReservationID)
			if tc.err != nil {
				expect.WillReturnError(tc.err)
				mock.ExpectRollback()
			} else if tc.rows == 1 {
				expect.WillReturnResult(sqlmock.NewResult(0, tc.rows))
				mock.ExpectExec(regexp.QuoteMeta(insertPaymentAttemptHistory)).WithArgs("order-1", attempt.ID, attempt.ReservationID, PendingPayment).WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectCommit()
			} else {
				expect.WillReturnResult(sqlmock.NewResult(0, tc.rows))
				mock.ExpectCommit()
			}

			reset, err := NewMySQLRepository(db).ResetPaymentClaim(context.Background(), 7, "order-1", attempt)
			if (err != nil) != tc.wantErr || reset != tc.reset {
				t.Fatalf("ResetPaymentClaim() = %v, %v; want %v, error=%v", reset, err, tc.reset, tc.wantErr)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestMySQLPaymentRepositoryResetPaymentClaimRecordsTerminalAttempt(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	attempt := PaymentAttempt{ID: "attempt-1", ReservationID: "reservation-1"}
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(resetPaymentOrder)).WithArgs(PendingPayment, uint64(7), "order-1", PaymentProcessing, attempt.ID, attempt.ReservationID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(insertPaymentAttemptHistory)).WithArgs("order-1", attempt.ID, attempt.ReservationID, PendingPayment).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	reset, err := NewMySQLRepository(db).ResetPaymentClaim(context.Background(), 7, "order-1", attempt)
	if err != nil || !reset {
		t.Fatalf("ResetPaymentClaim() = %v, %v; want true, nil", reset, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMySQLPaymentRepositoryResetPaymentClaimRollsBackWhenHistoryInsertFails(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	attempt := PaymentAttempt{ID: "attempt-1", ReservationID: "reservation-1"}
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(resetPaymentOrder)).WithArgs(PendingPayment, uint64(7), "order-1", PaymentProcessing, attempt.ID, attempt.ReservationID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(insertPaymentAttemptHistory)).WithArgs("order-1", attempt.ID, attempt.ReservationID, PendingPayment).WillReturnError(errors.New("history unavailable"))
	mock.ExpectRollback()

	reset, err := NewMySQLRepository(db).ResetPaymentClaim(context.Background(), 7, "order-1", attempt)
	if err == nil || reset {
		t.Fatalf("ResetPaymentClaim() = %v, %v; want false, error", reset, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMySQLPaymentRepositoryPaymentSettlementStatusReadsResetAttempt(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	const query = `SELECT status FROM payment_attempt_history WHERE order_no = ? AND payment_attempt_id = ? UNION ALL SELECT status FROM orders WHERE order_no = ? AND payment_attempt_id = ? LIMIT 1`
	mock.ExpectQuery(regexp.QuoteMeta(query)).WithArgs("order-1", "attempt-reset", "order-1", "attempt-reset").WillReturnRows(
		sqlmock.NewRows([]string{"status"}).AddRow(PendingPayment),
	)

	got, err := NewMySQLRepository(db).PaymentSettlementStatus(context.Background(), "order-1", "attempt-reset")
	if err != nil || got != PendingPayment {
		t.Fatalf("PaymentSettlementStatus() = %q, %v; want %q, nil", got, err, PendingPayment)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMySQLPaymentRepositoryPaymentSettlementStatusRejectsUnrelatedAttempt(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	const query = `SELECT status FROM payment_attempt_history WHERE order_no = ? AND payment_attempt_id = ? UNION ALL SELECT status FROM orders WHERE order_no = ? AND payment_attempt_id = ? LIMIT 1`
	mock.ExpectQuery(regexp.QuoteMeta(query)).WithArgs("order-1", "unrelated-attempt", "order-1", "unrelated-attempt").WillReturnRows(sqlmock.NewRows([]string{"status"}))

	_, err = NewMySQLRepository(db).PaymentSettlementStatus(context.Background(), "order-1", "unrelated-attempt")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("PaymentSettlementStatus() error = %v; want ErrNotFound", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMySQLPaymentRepositoryGetPaymentOrderReadsCurrentDurableState(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	order := persistedOrder()
	order.ID, order.OrderNo, order.Status, order.TotalAmount, order.PaidAmount = 11, "order-1", Paid, "12.00", "12.00"
	mock.ExpectQuery(regexp.QuoteMeta(queryPaymentOrder)).WithArgs(uint64(7), "order-1").WillReturnRows(
		sqlmock.NewRows(paymentOrderColumns()).AddRow(uint64(11), "order-1", uint64(7), Paid, "12.00", "12.00", "attempt-1", "reservation-1"),
	)
	expectOrderByNumber(mock, order)

	got, err := NewMySQLRepository(db).GetPaymentOrder(context.Background(), 7, "order-1")
	if err != nil || got.Status != Paid || got.OrderNo != "order-1" || got.Payment != (PaymentAttempt{ID: "attempt-1", ReservationID: "reservation-1"}) || len(got.Items) != 1 || got.Items[0].AppliedPromotion == nil || len(got.Items[0].CandidatePromotions) != 2 || got.Shipping.Detail != order.Shipping.Detail {
		t.Fatalf("GetPaymentOrder() = %#v, %v", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMySQLPaymentRepositorySettleReturnsPaidOrderSnapshotsForReplay(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	paid := PaymentAttempt{ID: "attempt-1", ReservationID: "reservation-1"}
	order := persistedOrder()
	order.ID, order.OrderNo, order.Status, order.TotalAmount, order.PaidAmount = 11, "order-1", Paid, "12.00", "12.00"

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(queryPaymentOrderForUpdate)).WithArgs(uint64(7), "order-1").WillReturnRows(
		sqlmock.NewRows(paymentOrderColumns()).AddRow(uint64(11), "order-1", uint64(7), Paid, "12.00", "12.00", paid.ID, paid.ReservationID),
	)
	mock.ExpectCommit()
	expectOrderByNumber(mock, order)

	got, err := NewMySQLRepository(db).SettleWalletPayment(context.Background(), 7, "order-1", PaymentAttempt{ID: "attempt-2", ReservationID: "reservation-2"})
	if err != nil || got.Status != Paid || got.Payment != paid || len(got.Items) != 1 || got.Items[0].AppliedPromotion == nil || len(got.Items[0].CandidatePromotions) != 2 || got.Shipping.Detail != order.Shipping.Detail {
		t.Fatalf("SettleWalletPayment() = %#v, %v", got, err)
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
				mock.ExpectExec(regexp.QuoteMeta(insertReservationConfirmationOutbox)).WithArgs(sqlmock.AnyArg(), "INVENTORY_RESERVATION", attempt.ReservationID, "CONFIRM", "inventory.reservation.confirm", attempt.ReservationID, `{"event_id":"reservation-1","reservation_id":"reservation-1","order_no":"order-1","payment_attempt_id":"attempt-1","version":1}`).WillReturnError(errors.New("outbox unavailable"))
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
