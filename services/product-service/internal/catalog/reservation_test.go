package catalog

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/yuyu945/AI-Shopping/internal/platform/apperror"
)

type fakeReservationStore struct {
	reserveFn func(context.Context, ReserveStockInput, time.Time, time.Time) (Reservation, ReservationMutation, error)
	confirmFn func(context.Context, string, time.Time) (Reservation, error)
	releaseFn func(context.Context, string, time.Time, time.Time) (Reservation, ReservationMutation, error)
	getFn     func(context.Context, string) (Reservation, error)
}

func (f fakeReservationStore) ReserveStock(ctx context.Context, in ReserveStockInput, at, executeAt time.Time) (Reservation, ReservationMutation, error) {
	return f.reserveFn(ctx, in, at, executeAt)
}
func (f fakeReservationStore) ConfirmReservation(ctx context.Context, id string, at time.Time) (Reservation, error) {
	return f.confirmFn(ctx, id, at)
}
func (f fakeReservationStore) ReleaseReservation(ctx context.Context, id string, at, executeAt time.Time) (Reservation, ReservationMutation, error) {
	return f.releaseFn(ctx, id, at, executeAt)
}
func (f fakeReservationStore) GetReservation(ctx context.Context, id string) (Reservation, error) {
	return f.getFn(ctx, id)
}

func TestReservationServiceReserveRejectsInvalidInputBeforeStore(t *testing.T) {
	cases := []ReserveStockInput{
		{},
		{ReservationID: "r", OrderNo: "o", PaymentAttemptID: "p", ExpiresAt: time.Now().Add(time.Minute)},
		{ReservationID: "r", OrderNo: "o", PaymentAttemptID: "p", Items: []ReservationItem{{SKUID: 1, Quantity: 1}}, ExpiresAt: time.Time{}},
		{ReservationID: "r", OrderNo: "o", PaymentAttemptID: "p", Items: []ReservationItem{{SKUID: 1, Quantity: 1}, {SKUID: 1, Quantity: 2}}, ExpiresAt: time.Now().Add(time.Minute)},
	}
	for _, input := range cases {
		storeCalled := false
		service, err := NewReservationService(fakeReservationStore{reserveFn: func(context.Context, ReserveStockInput, time.Time, time.Time) (Reservation, ReservationMutation, error) {
			storeCalled = true
			return Reservation{}, ReservationMutation{}, nil
		}}, nil, time.Now, time.Second, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		_, err = service.ReserveStock(context.Background(), input)
		var appErr *apperror.Error
		if !errors.As(err, &appErr) || appErr.Code != apperror.InvalidArgument || storeCalled {
			t.Fatalf("ReserveStock(%#v) error=%v storeCalled=%v", input, err, storeCalled)
		}
	}
}

func TestReservationServiceReserveDeletesCommittedCacheKeys(t *testing.T) {
	cache := newFakeDetailCache()
	now := time.Date(2026, time.August, 22, 9, 0, 0, 0, time.UTC)
	service, err := NewReservationService(fakeReservationStore{reserveFn: func(_ context.Context, input ReserveStockInput, at, executeAt time.Time) (Reservation, ReservationMutation, error) {
		if at != now || executeAt != now.Add(time.Second) || input.Items[0].SKUID != 7 {
			t.Fatalf("input=%#v at=%v executeAt=%v", input, at, executeAt)
		}
		return Reservation{ReservationID: input.ReservationID, Status: ReservationReserved}, ReservationMutation{CacheKeys: []string{"product:v1:detail:1", "product:v1:detail:1:sku:7"}}, nil
	}}, cache, func() time.Time { return now }, time.Second, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.ReserveStock(context.Background(), ReserveStockInput{ReservationID: "r", OrderNo: "o", PaymentAttemptID: "p", Items: []ReservationItem{{SKUID: 7, Quantity: 1}}, ExpiresAt: now.Add(time.Minute)})
	if err != nil || got.Status != ReservationReserved || !sameStrings(cache.dels, []string{"product:v1:detail:1", "product:v1:detail:1:sku:7"}) {
		t.Fatalf("ReserveStock()=%#v, %v, deletes=%v", got, err, cache.dels)
	}
}

func TestReservationServiceMapsStoreErrorsToStableCodes(t *testing.T) {
	service, err := NewReservationService(fakeReservationStore{reserveFn: func(context.Context, ReserveStockInput, time.Time, time.Time) (Reservation, ReservationMutation, error) {
		return Reservation{}, ReservationMutation{}, ErrReservationOutOfStock
	}}, nil, time.Now, time.Second, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.ReserveStock(context.Background(), ReserveStockInput{ReservationID: "r", OrderNo: "o", PaymentAttemptID: "p", Items: []ReservationItem{{SKUID: 1, Quantity: 1}}, ExpiresAt: time.Now().Add(time.Minute)})
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Code != apperror.OutOfStock {
		t.Fatalf("error=%v", err)
	}
}
