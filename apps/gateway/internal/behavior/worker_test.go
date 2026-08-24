package behavior

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/segmentio/kafka-go"
)

func TestWorkerPublishesClaimedBehaviorEvent(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(claimBehaviorOutbox)).
		WithArgs("claim-1", sqlmock.AnyArg(), 1).
		WillReturnRows(sqlmock.NewRows(leasedColumns()).AddRow(uint64(10), "event-1", uint64(7), "product.viewed", BehaviorEventsTopic, "7", []byte(`{"event_id":"event-1"}`)))
	mock.ExpectCommit()
	mock.ExpectExec(regexp.QuoteMeta(markBehaviorOutboxPublished)).
		WithArgs("claim-1", uint64(10)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	publisher := &fakePublisher{}
	worker := NewWorker(NewMySQLRepository(db), publisher, Config{BatchSize: 1, LeaseDuration: time.Second, CallTimeout: time.Second, Now: func() time.Time { return now }, ClaimToken: func() string { return "claim-1" }})

	if err := worker.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if publisher.calls != 1 || string(publisher.message.Key) != "7" || publisher.message.Topic != BehaviorEventsTopic {
		t.Fatalf("publisher=%#v", publisher)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

type fakePublisher struct {
	calls   int
	message kafka.Message
	err     error
}

func (p *fakePublisher) Publish(_ context.Context, message kafka.Message) error {
	p.calls++
	p.message = message
	return p.err
}
