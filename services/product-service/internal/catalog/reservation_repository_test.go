package catalog

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestReservationRepositoryReserveRollsBackAllSKUsWhenOneIsShort(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Date(2026, time.August, 22, 9, 0, 0, 0, time.UTC)
	expires := now.Add(time.Minute)
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(reservationRowsForUpdateQuery)).WithArgs("r-1").WillReturnRows(sqlmock.NewRows(reservationColumns))
	mock.ExpectExec(regexp.QuoteMeta(reserveInventoryQuery)).WithArgs(uint32(2), uint64(7), uint32(2)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(reserveInventoryQuery)).WithArgs(uint32(1), uint64(8), uint32(1)).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()
	_, _, err = NewReservationRepository(db).ReserveStock(context.Background(), ReserveStockInput{ReservationID: "r-1", OrderNo: "o-1", PaymentAttemptID: "p-1", Items: []ReservationItem{{SKUID: 8, Quantity: 1}, {SKUID: 7, Quantity: 2}}, ExpiresAt: expires}, now, now.Add(time.Second))
	if !errors.Is(err, ErrReservationOutOfStock) {
		t.Fatalf("error=%v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestReservationRepositoryReserveReturnsExistingIdenticalPayload(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Date(2026, time.August, 22, 9, 0, 0, 0, time.UTC)
	expires := now.Add(time.Minute)
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(reservationRowsForUpdateQuery)).WithArgs("r-1").WillReturnRows(reservationRows(expires, ReservationReserved, []ReservationItem{{SKUID: 7, Quantity: 2}, {SKUID: 8, Quantity: 1}}))
	mock.ExpectCommit()
	got, mutation, err := NewReservationRepository(db).ReserveStock(context.Background(), ReserveStockInput{ReservationID: "r-1", OrderNo: "o-1", PaymentAttemptID: "p-1", Items: []ReservationItem{{SKUID: 8, Quantity: 1}, {SKUID: 7, Quantity: 2}}, ExpiresAt: expires}, now, now.Add(time.Second))
	if err != nil || got.Status != ReservationReserved || len(mutation.CacheKeys) != 0 {
		t.Fatalf("got=%#v mutation=%#v err=%v", got, mutation, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestReservationRepositoryReleaseRestoresInventoryOnlyOnce(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Date(2026, time.August, 22, 9, 0, 0, 0, time.UTC)
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(reservationRowsForUpdateQuery)).WithArgs("r-1").WillReturnRows(reservationRows(now.Add(time.Minute), ReservationReserved, []ReservationItem{{SKUID: 7, Quantity: 2}}))
	mock.ExpectExec(regexp.QuoteMeta(releaseInventoryQuery)).WithArgs(uint32(2), uint64(7)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(releaseReservationRowsQuery)).WithArgs(ReservationReleased, now, "r-1", ReservationReserved).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(regexp.QuoteMeta(reservationProductIDQuery)).WithArgs(uint64(7)).WillReturnRows(sqlmock.NewRows([]string{"product_id"}).AddRow(uint64(10)))
	mock.ExpectExec(regexp.QuoteMeta(insertInvalidationTaskQuery)).WithArgs(ProductCacheKey(10, nil), now.Add(time.Second)).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(regexp.QuoteMeta(insertInvalidationTaskQuery)).WithArgs(ProductCacheKey(10, uint64Ptr(7)), now.Add(time.Second)).WillReturnResult(sqlmock.NewResult(2, 1))
	mock.ExpectCommit()
	got, mutation, err := NewReservationRepository(db).ReleaseReservation(context.Background(), "r-1", now, now.Add(time.Second))
	if err != nil || got.Status != ReservationReleased || len(mutation.CacheKeys) != 2 {
		t.Fatalf("got=%#v mutation=%#v err=%v", got, mutation, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestReservationRepositoryRejectsDuplicateReservationWithDifferentPayload(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Date(2026, time.August, 22, 9, 0, 0, 0, time.UTC)
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(reservationRowsForUpdateQuery)).WithArgs("r-1").WillReturnRows(reservationRows(now.Add(time.Minute), ReservationReserved, []ReservationItem{{SKUID: 7, Quantity: 1}}))
	mock.ExpectRollback()
	_, _, err = NewReservationRepository(db).ReserveStock(context.Background(), ReserveStockInput{ReservationID: "r-1", OrderNo: "o-1", PaymentAttemptID: "p-1", Items: []ReservationItem{{SKUID: 7, Quantity: 2}}, ExpiresAt: now.Add(time.Minute)}, now, now.Add(time.Second))
	if !errors.Is(err, ErrReservationConflict) {
		t.Fatalf("error=%v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestReservationRepositoryConfirmReplayDoesNotRepeatTransition(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Date(2026, time.August, 22, 9, 0, 0, 0, time.UTC)
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(reservationRowsForUpdateQuery)).WithArgs("r-1").WillReturnRows(reservationRows(now.Add(time.Minute), ReservationConfirmed, []ReservationItem{{SKUID: 7, Quantity: 1}}))
	mock.ExpectCommit()
	got, err := NewReservationRepository(db).ConfirmReservation(context.Background(), "r-1", now)
	if err != nil || got.Status != ReservationConfirmed {
		t.Fatalf("ConfirmReservation()=%#v err=%v", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestReservationRepositoryReleaseReplayDoesNotRestoreInventoryTwice(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Date(2026, time.August, 22, 9, 0, 0, 0, time.UTC)
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(reservationRowsForUpdateQuery)).WithArgs("r-1").WillReturnRows(reservationRows(now.Add(time.Minute), ReservationReleased, []ReservationItem{{SKUID: 7, Quantity: 1}}))
	mock.ExpectCommit()
	got, mutation, err := NewReservationRepository(db).ReleaseReservation(context.Background(), "r-1", now, now.Add(time.Second))
	if err != nil || got.Status != ReservationReleased || len(mutation.CacheKeys) != 0 {
		t.Fatalf("ReleaseReservation()=%#v mutation=%#v err=%v", got, mutation, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestReservationRepositoryGetReturnsItemsInStableSKUOrder(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Date(2026, time.August, 22, 9, 0, 0, 0, time.UTC)
	mock.ExpectQuery(regexp.QuoteMeta(reservationRowsQuery)).WithArgs("r-1").WillReturnRows(reservationRows(now.Add(time.Minute), ReservationReserved, []ReservationItem{{SKUID: 7, Quantity: 1}, {SKUID: 8, Quantity: 2}}))
	got, err := NewReservationRepository(db).GetReservation(context.Background(), "r-1")
	if err != nil || len(got.Items) != 2 || got.Items[0].SKUID != 7 || got.Items[1].SKUID != 8 {
		t.Fatalf("GetReservation()=%#v err=%v", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func reservationRows(expires time.Time, status ReservationStatus, items []ReservationItem) *sqlmock.Rows {
	rows := sqlmock.NewRows(reservationColumns)
	for _, item := range items {
		rows.AddRow("r-1", "o-1", "p-1", item.SKUID, item.Quantity, status, expires, nil, nil)
	}
	return rows
}

var reservationColumns = []string{"reservation_id", "order_no", "payment_attempt_id", "sku_id", "quantity", "status", "expires_at", "confirmed_at", "released_at"}
