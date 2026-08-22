package catalog

import (
	"context"
	"testing"
	"time"
)

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
