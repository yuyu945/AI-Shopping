package outbox

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestConfirmationOutboxRetriesWithoutReleasingPaidReservation(t *testing.T) {
	repository := &fakeRepository{events: []Event{{ID: 1, EventID: "event-1", ReservationID: "reservation-1"}}}
	publisher := &fakePublisher{err: errors.New("product unavailable")}
	worker := NewWorker(repository, publisher, Config{BatchSize: 1})

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
	if publisher.confirmCalls != 2 || repository.releaseCalls != 0 {
		t.Fatalf("confirm=%d release=%d", publisher.confirmCalls, repository.releaseCalls)
	}
}

type fakeRepository struct {
	events       []Event
	releaseCalls int
}

func (r *fakeRepository) LeasePending(context.Context, int) ([]Event, error) {
	for i := range r.events {
		if r.events[i].Status == "" || r.events[i].Status == Pending {
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
	confirmCalls int
}

func (p *fakePublisher) ConfirmReservation(context.Context, string) error {
	p.confirmCalls++
	return p.err
}
