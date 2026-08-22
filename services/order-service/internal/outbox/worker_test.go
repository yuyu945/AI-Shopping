package outbox

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestConfirmationOutboxRetriesWithoutReleasingPaidReservation(t *testing.T) {
	repository := &fakeRepository{events: []Event{{ID: 1, EventID: "event-1", ReservationID: "reservation-1", OrderNo: "order-1", PaymentAttemptID: "attempt-1", Version: 1}}}
	publisher := &fakePublisher{err: errors.New("product unavailable")}
	worker := NewWorker(repository, publisher, Config{BatchSize: 1, LeaseDuration: time.Minute})

	if err := worker.RunOnce(context.Background()); err == nil {
		t.Fatal("first publish must fail")
	}
	if got := repository.events[0].Status; got != Pending {
		t.Fatalf("status after first failure = %s, want %s", got, Pending)
	}
	if repository.releaseCalls != 0 {
		t.Fatalf("release calls = %d, want 0", repository.releaseCalls)
	}
	publisher.err = nil
	if err := worker.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := repository.events[0].Status; got != Published {
		t.Fatalf("status after retry = %s, want %s", got, Published)
	}
	if publisher.publishCalls != 2 || repository.releaseCalls != 0 {
		t.Fatalf("publish=%d release=%d", publisher.publishCalls, repository.releaseCalls)
	}
}

func TestConfirmationOutboxReclaimsExpiredProcessingLease(t *testing.T) {
	repository := &fakeRepository{events: []Event{{ID: 1, EventID: "event-1", ReservationID: "reservation-1", Status: Processing, LeaseUntil: time.Now().Add(-time.Minute)}}}
	publisher := &fakePublisher{}
	worker := NewWorker(repository, publisher, Config{BatchSize: 1, LeaseDuration: time.Minute})

	if err := worker.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := repository.events[0].Status; got != Published {
		t.Fatalf("status after expired lease = %s, want %s", got, Published)
	}
}

type fakeRepository struct {
	events       []Event
	releaseCalls int
}

func (r *fakeRepository) LeasePending(_ context.Context, _ int, now time.Time, _ time.Duration) ([]Event, error) {
	for i := range r.events {
		if r.events[i].Status == "" || r.events[i].Status == Pending || (r.events[i].Status == Processing && r.events[i].LeaseUntil.Before(now)) {
			r.events[i].Status = Processing
			return []Event{r.events[i]}, nil
		}
	}
	return nil, nil
}
func (r *fakeRepository) MarkPublished(_ context.Context, id uint64) error {
	for i := range r.events {
		if r.events[i].ID == id {
			r.events[i].Status = Published
		}
	}
	return nil
}
func (r *fakeRepository) Retry(_ context.Context, id uint64, _ time.Time) error {
	for i := range r.events {
		if r.events[i].ID == id {
			r.events[i].Status = Pending
		}
	}
	return nil
}

type fakePublisher struct {
	err          error
	publishCalls int
}

func (p *fakePublisher) Publish(context.Context, Message) error {
	p.publishCalls++
	return p.err
}
