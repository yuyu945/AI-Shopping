package catalog

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestConfirmationConsumerSkipsDuplicateEvent(t *testing.T) {
	store := &fakeConfirmationStore{}
	consumer := NewConfirmationConsumer(store, "product-inventory-confirmation")
	event := ConfirmationEvent{EventID: "event-1", ReservationID: "reservation-1", OrderNo: "order-1", PaymentAttemptID: "attempt-1", Version: 1}

	if err := consumer.Handle(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if err := consumer.Handle(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if store.calls != 2 {
		t.Fatalf("consume calls = %d, want 2", store.calls)
	}
}

func TestRetryConfirmationRetriesTransientFailure(t *testing.T) {
	calls := 0
	if err := retryConfirmation(context.Background(), func() error {
		calls++
		if calls < 3 {
			return errors.New("temporary database failure")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3", calls)
	}
}

func TestRetryConfirmationStopsWhenContextIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := retryConfirmation(ctx, func() error { return errors.New("temporary database failure") })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context canceled", err)
	}
}

func TestReservationRepositoryConfirmConsumedRollsBackWhenConfirmationFails(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Date(2026, time.August, 22, 9, 0, 0, 0, time.UTC)
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(insertReservationConsumptionQuery)).WithArgs("event-1", inventoryConfirmationConsumerGroup).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery(regexp.QuoteMeta(reservationRowsForUpdateQuery)).WithArgs("r-1").WillReturnRows(reservationRows(now.Add(time.Minute), ReservationReserved, []ReservationItem{{SKUID: 7, Quantity: 1}}))
	mock.ExpectExec(regexp.QuoteMeta(confirmReservationRowsQuery)).WithArgs(ReservationConfirmed, now, "r-1", ReservationReserved).WillReturnError(errors.New("database unavailable"))
	mock.ExpectRollback()
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(insertReservationConsumptionQuery)).WithArgs("event-1", inventoryConfirmationConsumerGroup).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery(regexp.QuoteMeta(reservationRowsForUpdateQuery)).WithArgs("r-1").WillReturnRows(reservationRows(now.Add(time.Minute), ReservationReserved, []ReservationItem{{SKUID: 7, Quantity: 1}}))
	mock.ExpectExec(regexp.QuoteMeta(confirmReservationRowsQuery)).WithArgs(ReservationConfirmed, now, "r-1", ReservationReserved).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err = NewReservationRepository(db).ConfirmConsumed(context.Background(), "event-1", inventoryConfirmationConsumerGroup, "r-1", now)
	if err == nil {
		t.Fatal("ConfirmConsumed() error = nil, want confirmation failure")
	}
	if err := NewReservationRepository(db).ConfirmConsumed(context.Background(), "event-1", inventoryConfirmationConsumerGroup, "r-1", now); err != nil {
		t.Fatalf("retry ConfirmConsumed() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestReservationRepositoryConfirmConsumedRollsBackWhenReservationIsUnknown(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(insertReservationConsumptionQuery)).WithArgs("event-1", inventoryConfirmationConsumerGroup).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery(regexp.QuoteMeta(reservationRowsForUpdateQuery)).WithArgs("missing").WillReturnRows(sqlmock.NewRows(reservationColumns))
	mock.ExpectRollback()

	err = NewReservationRepository(db).ConfirmConsumed(context.Background(), "event-1", inventoryConfirmationConsumerGroup, "missing", time.Now())
	if !errors.Is(err, ErrReservationNotFound) {
		t.Fatalf("error = %v, want unknown reservation", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestReservationRepositoryConfirmConsumedDuplicateSkipsSecondConfirmation(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(insertReservationConsumptionQuery)).WithArgs("event-1", inventoryConfirmationConsumerGroup).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	if err := NewReservationRepository(db).ConfirmConsumed(context.Background(), "event-1", inventoryConfirmationConsumerGroup, "r-1", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

type fakeConfirmationStore struct{ calls int }

func (s *fakeConfirmationStore) ConfirmConsumed(context.Context, string, string, string, time.Time) error {
	s.calls++
	return nil
}
