package catalog

import (
	"context"
	"testing"
)

func TestConfirmationConsumerSkipsDuplicateEvent(t *testing.T) {
	store := &fakeConsumptionStore{}
	reservations := &fakeReservationConfirmer{}
	consumer := NewConfirmationConsumer(store, reservations, "product-inventory-confirmation")
	event := ConfirmationEvent{EventID: "event-1", ReservationID: "reservation-1", OrderNo: "order-1", PaymentAttemptID: "attempt-1", Version: 1}

	if err := consumer.Handle(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if err := consumer.Handle(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if reservations.calls != 1 {
		t.Fatalf("confirm calls = %d, want 1", reservations.calls)
	}
}

type fakeConsumptionStore struct{ seen map[string]bool }

func (s *fakeConsumptionStore) Record(_ context.Context, eventID, group string) (bool, error) {
	if s.seen == nil {
		s.seen = map[string]bool{}
	}
	key := eventID + "/" + group
	if s.seen[key] {
		return false, nil
	}
	s.seen[key] = true
	return true, nil
}

type fakeReservationConfirmer struct{ calls int }

func (s *fakeReservationConfirmer) ConfirmReservation(context.Context, string) (Reservation, error) {
	s.calls++
	return Reservation{Status: ReservationConfirmed}, nil
}
