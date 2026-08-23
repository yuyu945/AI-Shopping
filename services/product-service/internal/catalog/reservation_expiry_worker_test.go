package catalog

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

const leaseExpiredReservationsQuery = `SELECT reservation_id, order_no, payment_attempt_id FROM inventory_reservations WHERE status='RESERVED' AND expires_at<=? AND (next_retry_at IS NULL OR next_retry_at<=?) AND (expiry_lease_until IS NULL OR expiry_lease_until<=?) GROUP BY reservation_id,order_no,payment_attempt_id ORDER BY reservation_id LIMIT ? FOR UPDATE SKIP LOCKED`
const releaseExpiredReservationItemsQuery = `SELECT sku_id, quantity FROM inventory_reservations WHERE reservation_id=? AND status='RESERVED' AND expiry_lease_token=? FOR UPDATE`
const releaseExpiredReservationRowsQuery = `UPDATE inventory_reservations SET status='RELEASED', released_at=?, expiry_lease_token=NULL, expiry_lease_until=NULL WHERE reservation_id=? AND status='RESERVED' AND expiry_lease_token=?`

type expiryStoreStub struct {
	reservations                  []ExpiredReservation
	confirmed, released, retained int
}

func (s *expiryStoreStub) LeaseExpired(context.Context, int, time.Time, time.Duration) ([]ExpiredReservation, error) {
	return s.reservations, nil
}
func (s *expiryStoreStub) ConfirmExpired(context.Context, ExpiredReservation, time.Time) error {
	s.confirmed++
	return nil
}
func (s *expiryStoreStub) ReleaseExpired(context.Context, ExpiredReservation, time.Time) error {
	s.released++
	return nil
}
func (s *expiryStoreStub) RetryExpired(context.Context, ExpiredReservation, time.Time) error {
	s.retained++
	return nil
}

type settlementStub struct {
	state SettlementState
	err   error
}

func (s settlementStub) GetPaymentSettlementStatus(context.Context, string, string) (SettlementState, error) {
	return s.state, s.err
}
func expired() ExpiredReservation {
	return ExpiredReservation{ReservationID: "r-1", OrderNo: "o-1", PaymentAttemptID: "p-1", LeaseToken: "lease"}
}
func newExpiryWorker(store *expiryStoreStub, client settlementStub) *ReservationExpiryWorker {
	return NewReservationExpiryWorker(store, client, ExpiryWorkerConfig{BatchSize: 1, LeaseDuration: time.Minute, CallTimeout: time.Second, RetryDelay: time.Minute})
}
func TestExpiredReservationConfirmsPaid(t *testing.T) {
	store := &expiryStoreStub{reservations: []ExpiredReservation{expired()}}
	if err := newExpiryWorker(store, settlementStub{state: SettlementPaid}).RunOnce(context.Background()); err != nil || store.confirmed != 1 {
		t.Fatalf("err=%v confirms=%d", err, store.confirmed)
	}
}
func TestExpiredReservationReleasesTerminalUnpaid(t *testing.T) {
	for _, state := range []SettlementState{SettlementPendingPayment, SettlementClosed} {
		t.Run(string(state), func(t *testing.T) {
			store := &expiryStoreStub{reservations: []ExpiredReservation{expired()}}
			if err := newExpiryWorker(store, settlementStub{state: state}).RunOnce(context.Background()); err != nil || store.released != 1 {
				t.Fatalf("err=%v releases=%d", err, store.released)
			}
		})
	}
}
func TestExpiredReservationRetainsProcessingAndTimeout(t *testing.T) {
	for _, c := range []settlementStub{{state: SettlementProcessing}, {err: context.DeadlineExceeded}} {
		store := &expiryStoreStub{reservations: []ExpiredReservation{expired()}}
		if err := newExpiryWorker(store, c).RunOnce(context.Background()); err != nil || store.retained != 1 || store.released != 0 {
			t.Fatalf("err=%v retained=%d released=%d", err, store.retained, store.released)
		}
	}
}

func TestMySQLExpiryStoreLeaseExpiredSkipsActiveLeases(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Date(2026, time.August, 23, 10, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(leaseExpiredReservationsQuery)).WithArgs(now, now, now, 20).WillReturnRows(sqlmock.NewRows([]string{"reservation_id", "order_no", "payment_attempt_id"}))
	mock.ExpectCommit()

	got, err := NewMySQLExpiryStore(db).LeaseExpired(context.Background(), 20, now, time.Minute)
	if err != nil || len(got) != 0 {
		t.Fatalf("LeaseExpired() = %#v, %v; want empty, nil", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMySQLExpiryStoreReleaseExpiredCreatesInvalidationTasks(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Date(2026, time.August, 23, 10, 0, 0, 0, time.UTC)
	expired := ExpiredReservation{ReservationID: "r-1", LeaseToken: "lease-1"}

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(releaseExpiredReservationItemsQuery)).WithArgs("r-1", "lease-1").WillReturnRows(sqlmock.NewRows([]string{"sku_id", "quantity"}).AddRow(uint64(7), uint32(2)))
	mock.ExpectExec(regexp.QuoteMeta(releaseInventoryQuery)).WithArgs(uint32(2), uint64(7)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(releaseExpiredReservationRowsQuery)).WithArgs(now, "r-1", "lease-1").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(regexp.QuoteMeta(reservationProductIDQuery)).WithArgs(uint64(7)).WillReturnRows(sqlmock.NewRows([]string{"product_id"}).AddRow(uint64(10)))
	mock.ExpectExec(regexp.QuoteMeta(insertInvalidationTaskQuery)).WithArgs(ProductCacheKey(10, nil), now).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(regexp.QuoteMeta(insertInvalidationTaskQuery)).WithArgs(ProductCacheKey(10, uint64Ptr(7)), now).WillReturnResult(sqlmock.NewResult(2, 1))
	mock.ExpectCommit()

	if err := NewMySQLExpiryStore(db).ReleaseExpired(context.Background(), expired, now); err != nil {
		t.Fatalf("ReleaseExpired() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMySQLExpiryStoreReleaseExpiredRejectsLostLeaseWithNoRows(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Date(2026, time.August, 23, 10, 0, 0, 0, time.UTC)
	expired := ExpiredReservation{ReservationID: "r-1", LeaseToken: "lease-1"}

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(releaseExpiredReservationItemsQuery)).WithArgs("r-1", "lease-1").WillReturnRows(sqlmock.NewRows([]string{"sku_id", "quantity"}))
	mock.ExpectRollback()

	err = NewMySQLExpiryStore(db).ReleaseExpired(context.Background(), expired, now)
	if err == nil || !strings.Contains(err.Error(), "reservation expiry release lease lost") {
		t.Fatalf("ReleaseExpired() error = %v, want lost lease error", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
