package recovery

import (
	"context"
	"errors"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/yuyu945/AI-Shopping/services/order-service/internal/order"
)

func TestMySQLStoreResetRecordsTerminalAttemptInSameTransaction(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	attempt := Attempt{UserID: 7, OrderNo: "order-1", Payment: order.PaymentAttempt{ID: "attempt-1", ReservationID: "reservation-1"}, LeaseToken: "lease-1"}

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(resetLeasedPaymentOrder)).WithArgs(attempt.UserID, attempt.OrderNo, attempt.Payment.ID, attempt.Payment.ReservationID, attempt.LeaseToken).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(insertPaymentAttemptHistory)).WithArgs(attempt.OrderNo, attempt.Payment.ID, attempt.Payment.ReservationID, order.PendingPayment).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	reset, err := NewMySQLStore(db).Reset(context.Background(), attempt)
	if err != nil || !reset {
		t.Fatalf("Reset() = %v, %v; want true, nil", reset, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMySQLStoreResetRollsBackWhenHistoryInsertFails(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	attempt := Attempt{UserID: 7, OrderNo: "order-1", Payment: order.PaymentAttempt{ID: "attempt-1", ReservationID: "reservation-1"}, LeaseToken: "lease-1"}

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(resetLeasedPaymentOrder)).WithArgs(attempt.UserID, attempt.OrderNo, attempt.Payment.ID, attempt.Payment.ReservationID, attempt.LeaseToken).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(insertPaymentAttemptHistory)).WithArgs(attempt.OrderNo, attempt.Payment.ID, attempt.Payment.ReservationID, order.PendingPayment).WillReturnError(errors.New("history unavailable"))
	mock.ExpectRollback()

	reset, err := NewMySQLStore(db).Reset(context.Background(), attempt)
	if err == nil || reset {
		t.Fatalf("Reset() = %v, %v; want false, error", reset, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
