package outbox

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestWorkerPublishesAndMarksDocumentIngestEvent(t *testing.T) {
	repo := &fakeRepo{events: []Event{{ID: 1, EventID: "event-1", Topic: "knowledge.document.ingest", EventKey: "doc_1", Payload: []byte(`{"event_id":"event-1"}`), Attempts: 1, ClaimToken: "claim"}}}
	publisher := &fakePublisher{}

	err := NewWorker(repo, publisher, Config{BatchSize: 1, LeaseDuration: time.Second, CallTimeout: 10 * time.Millisecond, MaxAttempts: 3}).RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if publisher.message.Topic != "knowledge.document.ingest" || publisher.message.Key != "doc_1" || string(publisher.message.Value) != `{"event_id":"event-1"}` {
		t.Fatalf("message=%#v", publisher.message)
	}
	if !repo.published || repo.dead || repo.retried {
		t.Fatalf("repo=%#v", repo)
	}
}

func TestWorkerRetriesPublishFailure(t *testing.T) {
	repo := &fakeRepo{events: []Event{{ID: 1, EventID: "event-1", Topic: "knowledge.document.ingest", EventKey: "doc_1", Payload: []byte(`{}`), Attempts: 1, ClaimToken: "claim"}}}
	publisher := &fakePublisher{err: errors.New("kafka unavailable")}

	err := NewWorker(repo, publisher, Config{BatchSize: 1, LeaseDuration: time.Second, CallTimeout: 10 * time.Millisecond, MaxAttempts: 3}).RunOnce(context.Background())
	if err == nil || !repo.retried || repo.published || repo.dead {
		t.Fatalf("err=%v repo=%#v", err, repo)
	}
	if !repo.nextRetry.After(time.Time{}) {
		t.Fatalf("next retry not set")
	}
}

func TestWorkerMarksDeadWhenMaxAttemptsReached(t *testing.T) {
	repo := &fakeRepo{events: []Event{{ID: 1, EventID: "event-1", Topic: "knowledge.document.ingest", EventKey: "doc_1", Payload: []byte(`{}`), Attempts: 3, ClaimToken: "claim"}}}
	publisher := &fakePublisher{err: errors.New("kafka unavailable")}

	err := NewWorker(repo, publisher, Config{BatchSize: 1, LeaseDuration: time.Second, CallTimeout: 10 * time.Millisecond, MaxAttempts: 3}).RunOnce(context.Background())
	if err == nil || !repo.dead || repo.retried || repo.published {
		t.Fatalf("err=%v repo=%#v", err, repo)
	}
}

type fakeRepo struct {
	events    []Event
	published bool
	retried   bool
	dead      bool
	nextRetry time.Time
}

func (r *fakeRepo) LeasePending(_ context.Context, limit int, _ time.Time, _ time.Duration) ([]Event, error) {
	if len(r.events) > limit {
		return r.events[:limit], nil
	}
	return r.events, nil
}

func (r *fakeRepo) MarkPublished(_ context.Context, id uint64, claimToken string) error {
	r.published = id == 1 && claimToken == "claim"
	return nil
}

func (r *fakeRepo) Retry(_ context.Context, id uint64, claimToken string, nextRetryAt time.Time, _ string) error {
	r.retried = id == 1 && claimToken == "claim"
	r.nextRetry = nextRetryAt
	return nil
}

func (r *fakeRepo) MarkDead(_ context.Context, id uint64, claimToken string, _ string) error {
	r.dead = id == 1 && claimToken == "claim"
	return nil
}

type fakePublisher struct {
	message Message
	err     error
}

func (p *fakePublisher) Publish(_ context.Context, message Message) error {
	p.message = message
	return p.err
}
