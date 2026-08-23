package recovery

import (
	"context"
	"testing"
	"time"

	"github.com/yuyu945/AI-Shopping/services/order-service/internal/order"
)

type fakeStore struct {
	attempts      []Attempt
	resets, dones int
	resetOK       bool
}

func (s *fakeStore) LeaseStale(context.Context, int, time.Time, time.Duration) ([]Attempt, error) {
	return s.attempts, nil
}
func (s *fakeStore) Reset(context.Context, Attempt) (bool, error)   { s.resets++; return s.resetOK, nil }
func (s *fakeStore) Done(context.Context, Attempt, time.Time) error { s.dones++; return nil }

type fakeReservations struct {
	get      func(context.Context, string) (order.Reservation, error)
	releases int
}

func (r *fakeReservations) GetReservation(ctx context.Context, id string) (order.Reservation, error) {
	return r.get(ctx, id)
}
func (r *fakeReservations) ReleaseReservation(context.Context, string) error {
	r.releases++
	return nil
}

type fakeSettler struct {
	calls int
	err   error
}

func (s *fakeSettler) SettleRecovered(context.Context, Attempt) error { s.calls++; return s.err }

func recoveryAttempt() Attempt {
	return Attempt{UserID: 7, OrderNo: "o-1", Payment: order.PaymentAttempt{ID: "p-1", ReservationID: "r-1"}, LeaseToken: "lease"}
}

func TestPaymentWorkerResetsMissingReservation(t *testing.T) {
	store := &fakeStore{attempts: []Attempt{recoveryAttempt()}, resetOK: true}
	w := NewWorker(store, &fakeReservations{get: func(context.Context, string) (order.Reservation, error) {
		return order.Reservation{}, order.ErrNotFound
	}}, &fakeSettler{}, Config{BatchSize: 1, LeaseDuration: time.Minute, CallTimeout: time.Second})
	if err := w.RunOnce(context.Background()); err != nil || store.resets != 1 || store.dones != 1 {
		t.Fatalf("err=%v resets=%d dones=%d", err, store.resets, store.dones)
	}
}
func TestPaymentWorkerSettlesReservedAttempt(t *testing.T) {
	store, settled := &fakeStore{attempts: []Attempt{recoveryAttempt()}, resetOK: true}, &fakeSettler{}
	w := NewWorker(store, &fakeReservations{get: func(context.Context, string) (order.Reservation, error) {
		return order.Reservation{ReservationID: "r-1", PaymentAttemptID: "p-1", Status: order.ReservationReserved}, nil
	}}, settled, Config{BatchSize: 1, LeaseDuration: time.Minute, CallTimeout: time.Second})
	if err := w.RunOnce(context.Background()); err != nil || settled.calls != 1 || store.resets != 0 {
		t.Fatalf("err=%v settled=%d resets=%d", err, settled.calls, store.resets)
	}
}
func TestPaymentWorkerDoesNotResetOnReservationTimeout(t *testing.T) {
	store := &fakeStore{attempts: []Attempt{recoveryAttempt()}, resetOK: true}
	w := NewWorker(store, &fakeReservations{get: func(context.Context, string) (order.Reservation, error) {
		return order.Reservation{}, context.DeadlineExceeded
	}}, &fakeSettler{}, Config{BatchSize: 1, LeaseDuration: time.Minute, CallTimeout: time.Second})
	if err := w.RunOnce(context.Background()); err == nil || store.resets != 0 || store.dones != 1 {
		t.Fatalf("err=%v resets=%d dones=%d", err, store.resets, store.dones)
	}
}
func TestPaymentWorkerResetsAndReleasesForBalanceFailure(t *testing.T) {
	store, reservations := &fakeStore{attempts: []Attempt{recoveryAttempt()}, resetOK: true}, &fakeReservations{get: func(context.Context, string) (order.Reservation, error) {
		return order.Reservation{ReservationID: "r-1", PaymentAttemptID: "p-1", Status: order.ReservationReserved}, nil
	}}
	w := NewWorker(store, reservations, &fakeSettler{err: order.ErrInsufficientBalance}, Config{BatchSize: 1, LeaseDuration: time.Minute, CallTimeout: time.Second})
	if err := w.RunOnce(context.Background()); err != nil || store.resets != 1 || reservations.releases != 1 {
		t.Fatalf("err=%v resets=%d releases=%d", err, store.resets, reservations.releases)
	}
}
func TestPaymentWorkerReportsLostLease(t *testing.T) {
	store := &fakeStore{attempts: []Attempt{recoveryAttempt()}, resetOK: false}
	w := NewWorker(store, &fakeReservations{get: func(context.Context, string) (order.Reservation, error) {
		return order.Reservation{}, order.ErrNotFound
	}}, &fakeSettler{}, Config{BatchSize: 1, LeaseDuration: time.Minute, CallTimeout: time.Second})
	if err := w.RunOnce(context.Background()); err == nil {
		t.Fatal("expected lost lease error")
	}
}
