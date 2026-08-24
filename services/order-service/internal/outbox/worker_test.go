package outbox

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestConfirmationOutboxRetriesWithoutReleasingPaidReservation(t *testing.T) {
	repository := &fakeRepository{events: []Event{confirmationEvent(1)}}
	publisher := &fakePublisher{err: errors.New("product unavailable")}
	worker := NewWorker(repository, publisher, Config{BatchSize: 1, LeaseDuration: time.Minute, CallTimeout: 100 * time.Millisecond})

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

func TestOutboxPublishesReviewEventsWithoutRemarshalling(t *testing.T) {
	payload := []byte(`{"event_id":"event-1","event_type":"review.submitted","review_no":"REV-1","version":1}`)
	repository := &fakeRepository{events: []Event{{ID: 1, EventID: "event-1", Topic: ReviewEventsTopic, Key: "21", Payload: payload}}}
	publisher := &fakePublisher{}
	worker := NewWorker(repository, publisher, Config{BatchSize: 1, LeaseDuration: time.Minute, CallTimeout: 100 * time.Millisecond})

	if err := worker.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if publisher.lastMessage.Topic != ReviewEventsTopic || publisher.lastMessage.Key != "21" || string(publisher.lastMessage.Value) != string(payload) {
		t.Fatalf("published message = %#v", publisher.lastMessage)
	}
}

func TestMySQLRepositoryLeasesOnlyAllowedOutboxTopics(t *testing.T) {
	if !strings.Contains(queryLeasePendingOutboxEvents([]string{ConfirmationTopic, ReviewEventsTopic}), "topic IN (?,?)") {
		t.Fatal("lease query must restrict allowed outbox topics")
	}
}

func TestConfirmationOutboxReclaimsExpiredProcessingLease(t *testing.T) {
	event := confirmationEvent(1)
	event.Status = Processing
	event.LeaseUntil = time.Now().Add(-time.Minute)
	repository := &fakeRepository{events: []Event{event}}
	publisher := &fakePublisher{}
	worker := NewWorker(repository, publisher, Config{BatchSize: 1, LeaseDuration: time.Minute, CallTimeout: 100 * time.Millisecond})

	if err := worker.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := repository.events[0].Status; got != Published {
		t.Fatalf("status after expired lease = %s, want %s", got, Published)
	}
}

func TestConfirmationOutboxTimesOutBlockedPublisherAndRetainsEventForRetry(t *testing.T) {
	repository := &fakeRepository{events: []Event{confirmationEvent(1)}}
	publisher := &blockedPublisher{}
	worker := NewWorker(repository, publisher, Config{BatchSize: 1, LeaseDuration: time.Minute, CallTimeout: 100 * time.Millisecond})

	startedAt := time.Now()
	err := worker.RunOnce(context.Background())
	if err == nil {
		t.Fatal("RunOnce() error = nil, want publisher timeout")
	}
	if elapsed := time.Since(startedAt); elapsed > 2*time.Second {
		t.Fatalf("RunOnce() blocked for %s, want bounded publisher call", elapsed)
	}
	if !publisher.sawDeadline {
		t.Fatal("Publish() context had no deadline")
	}
	if got := repository.events[0].Status; got != Pending {
		t.Fatalf("status after publisher timeout = %s, want %s", got, Pending)
	}
	if publisher.publishCalls != 1 {
		t.Fatalf("publish calls = %d, want 1", publisher.publishCalls)
	}
}

func TestConfigValidateRejectsCallTimeoutOutsideLeaseBatchBudget(t *testing.T) {
	tests := []struct {
		name   string
		config Config
	}{
		{name: "zero call timeout", config: Config{BatchSize: 1, LeaseDuration: time.Second}},
		{name: "batch exceeds lease", config: Config{BatchSize: 2, LeaseDuration: time.Second, CallTimeout: 250 * time.Millisecond}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.config.Validate(); err == nil {
				t.Fatal("Validate() error = nil, want invalid configuration")
			}
		})
	}
}

func TestMySQLRepositoryRejectsStaleClaimWithoutChangingNewOwnerLease(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repository := NewMySQLRepository(db)
	claimQuery := regexp.QuoteMeta("UPDATE outbox_events SET status = 'PUBLISHED', published_at = CURRENT_TIMESTAMP(3), next_retry_at = NULL, locked_at = NULL, lease_until = NULL, claim_token = NULL WHERE id = ? AND status = 'PROCESSING' AND claim_token = ?")
	mock.ExpectExec(claimQuery).WithArgs(uint64(7), "claim-a").WillReturnResult(sqlmock.NewResult(0, 0))
	if err := repository.MarkPublished(context.Background(), 7, "claim-a"); err == nil {
		t.Fatal("stale claim published event, want lease lost error")
	}
	mock.ExpectExec(claimQuery).WithArgs(uint64(7), "claim-b").WillReturnResult(sqlmock.NewResult(0, 1))
	if err := repository.MarkPublished(context.Background(), 7, "claim-b"); err != nil {
		t.Fatalf("new owner MarkPublished() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMySQLRepositoryRejectsStaleClaimRetryWithoutChangingNewOwnerLease(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	nextRetryAt := time.Date(2026, time.August, 22, 10, 0, 0, 0, time.UTC)
	retryQuery := regexp.QuoteMeta("UPDATE outbox_events SET status = 'PENDING', next_retry_at = ?, locked_at = NULL, lease_until = NULL, claim_token = NULL WHERE id = ? AND status = 'PROCESSING' AND claim_token = ?")
	mock.ExpectExec(retryQuery).WithArgs(nextRetryAt, uint64(7), "claim-a").WillReturnResult(sqlmock.NewResult(0, 0))
	if err := NewMySQLRepository(db).Retry(context.Background(), 7, "claim-a", nextRetryAt); err == nil {
		t.Fatal("stale claim retried event, want lease lost error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

type fakeRepository struct {
	events       []Event
	releaseCalls int
}

func confirmationEvent(id uint64) Event {
	return Event{
		ID:      id,
		EventID: "event-1",
		Topic:   ConfirmationTopic,
		Key:     "reservation-1",
		Payload: []byte(`{"event_id":"event-1","reservation_id":"reservation-1","order_no":"order-1","payment_attempt_id":"attempt-1","version":1}`),
	}
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
func (r *fakeRepository) MarkPublished(_ context.Context, id uint64, _ string) error {
	for i := range r.events {
		if r.events[i].ID == id {
			r.events[i].Status = Published
		}
	}
	return nil
}
func (r *fakeRepository) Retry(_ context.Context, id uint64, _ string, _ time.Time) error {
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
	lastMessage  Message
}

func (p *fakePublisher) Publish(_ context.Context, message Message) error {
	p.publishCalls++
	p.lastMessage = message
	return p.err
}

type blockedPublisher struct {
	publishCalls int
	sawDeadline  bool
}

func (p *blockedPublisher) Publish(ctx context.Context, _ Message) error {
	p.publishCalls++
	_, p.sawDeadline = ctx.Deadline()
	<-ctx.Done()
	return ctx.Err()
}
