package analytics

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestReviewEventValidation(t *testing.T) {
	valid := ReviewEvent{
		EventID:    "11111111-1111-4111-8111-111111111111",
		EventType:  "review.submitted",
		ReviewNo:   "REV-1",
		OrderNo:    "ORD-1",
		UserID:     7,
		ProductID:  21,
		SKUID:      101,
		Rating:     5,
		Content:    "good",
		OccurredAt: time.Now().UTC(),
		Version:    1,
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	invalid := valid
	invalid.Rating = 6
	if err := invalid.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want invalid rating")
	}
}

func TestMySQLRepositoryHandlesNewReviewEvent(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	event := validReviewEvent()
	occurredAt := event.OccurredAt.UTC().Truncate(time.Millisecond)

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(insertReviewAnalyticsConsumption)).
		WithArgs(event.EventID, ReviewConsumerGroup).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(insertReviewEventRecord)).
		WithArgs(event.EventID, event.ReviewNo, event.OrderNo, event.UserID, event.ProductID, event.SKUID, event.Rating, event.Content, occurredAt).
		WillReturnResult(sqlmock.NewResult(11, 1))
	mock.ExpectQuery(regexp.QuoteMeta(queryProductReviewStatsForUpdate)).
		WithArgs(event.ProductID).
		WillReturnRows(sqlmock.NewRows([]string{"review_count", "rating_sum"}))
	mock.ExpectExec(regexp.QuoteMeta(insertProductReviewStats)).
		WithArgs(event.ProductID, uint64(1), uint64(event.Rating), "5.00", occurredAt).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(updateReviewAnalyticsConsumptionSucceeded)).
		WithArgs(event.EventID, ReviewConsumerGroup).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if err := NewMySQLRepository(db).HandleReviewEvent(context.Background(), event); err != nil {
		t.Fatalf("HandleReviewEvent() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMySQLRepositorySkipsAlreadySucceededReviewEvent(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	event := validReviewEvent()

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(insertReviewAnalyticsConsumption)).
		WithArgs(event.EventID, ReviewConsumerGroup).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta(queryReviewAnalyticsConsumption)).
		WithArgs(event.EventID, ReviewConsumerGroup).
		WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("SUCCEEDED"))
	mock.ExpectCommit()

	if err := NewMySQLRepository(db).HandleReviewEvent(context.Background(), event); err != nil {
		t.Fatalf("HandleReviewEvent() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMySQLRepositoryRejectsAlreadyProcessingReviewEvent(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	event := validReviewEvent()

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(insertReviewAnalyticsConsumption)).
		WithArgs(event.EventID, ReviewConsumerGroup).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta(queryReviewAnalyticsConsumption)).
		WithArgs(event.EventID, ReviewConsumerGroup).
		WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("PROCESSING"))
	mock.ExpectRollback()

	err = NewMySQLRepository(db).HandleReviewEvent(context.Background(), event)
	if err == nil || err.Error() != "review event consumption is already processing" {
		t.Fatalf("HandleReviewEvent() error = %v, want processing conflict", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMySQLRepositoryRollsBackReviewEventOnStatsFailure(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	event := validReviewEvent()
	occurredAt := event.OccurredAt.UTC().Truncate(time.Millisecond)

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(insertReviewAnalyticsConsumption)).
		WithArgs(event.EventID, ReviewConsumerGroup).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(insertReviewEventRecord)).
		WithArgs(event.EventID, event.ReviewNo, event.OrderNo, event.UserID, event.ProductID, event.SKUID, event.Rating, event.Content, occurredAt).
		WillReturnResult(sqlmock.NewResult(11, 1))
	mock.ExpectQuery(regexp.QuoteMeta(queryProductReviewStatsForUpdate)).
		WithArgs(event.ProductID).
		WillReturnRows(sqlmock.NewRows([]string{"review_count", "rating_sum"}).AddRow(uint64(2), uint64(8)))
	mock.ExpectExec(regexp.QuoteMeta(updateProductReviewStats)).
		WithArgs(uint64(3), uint64(13), "4.33", occurredAt, occurredAt, event.ProductID).
		WillReturnError(errors.New("stats update failed"))
	mock.ExpectRollback()

	err = NewMySQLRepository(db).HandleReviewEvent(context.Background(), event)
	if err == nil {
		t.Fatal("HandleReviewEvent() error = nil, want stats failure")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestBehaviorEventValidation(t *testing.T) {
	event := validBehaviorEvent()
	if err := event.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	event.Payload = []byte(`{"phone":"13800000000"}`)
	if err := event.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want pii rejection")
	}
}

func TestMySQLRepositoryHandlesNewBehaviorEvent(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	event := validBehaviorEvent()
	occurredAt := event.OccurredAt.UTC().Truncate(time.Millisecond)

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(insertBehaviorAnalyticsConsumption)).
		WithArgs(event.EventID, BehaviorConsumerGroup).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(insertBehaviorEventRecord)).
		WithArgs(event.EventID, event.UserID, event.EventType, event.TraceID, event.ResourceType, event.ResourceID, string(event.Payload), occurredAt).
		WillReturnResult(sqlmock.NewResult(11, 1))
	mock.ExpectExec(regexp.QuoteMeta(updateBehaviorAnalyticsConsumptionSucceeded)).
		WithArgs(event.EventID, BehaviorConsumerGroup).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if err := NewMySQLRepository(db).HandleBehaviorEvent(context.Background(), event); err != nil {
		t.Fatalf("HandleBehaviorEvent() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMySQLRepositorySkipsAlreadySucceededBehaviorEvent(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	event := validBehaviorEvent()

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(insertBehaviorAnalyticsConsumption)).
		WithArgs(event.EventID, BehaviorConsumerGroup).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta(queryBehaviorAnalyticsConsumption)).
		WithArgs(event.EventID, BehaviorConsumerGroup).
		WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("SUCCEEDED"))
	mock.ExpectCommit()

	if err := NewMySQLRepository(db).HandleBehaviorEvent(context.Background(), event); err != nil {
		t.Fatalf("HandleBehaviorEvent() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMySQLRepositoryRecordsAnalyticsDeadLetter(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	record := DeadLetterRecord{Topic: BehaviorEventsTopic, EventKey: "7", Reason: "invalid_behavior_event", RawEventBase64: "e30="}

	mock.ExpectExec(regexp.QuoteMeta(insertAnalyticsDeadLetter)).
		WithArgs(record.Topic, record.EventKey, record.Reason, record.RawEventBase64).
		WillReturnResult(sqlmock.NewResult(1, 1))

	if err := NewMySQLRepository(db).RecordDeadLetter(context.Background(), record); err != nil {
		t.Fatalf("RecordDeadLetter() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func validReviewEvent() ReviewEvent {
	return ReviewEvent{
		EventID:    "11111111-1111-4111-8111-111111111111",
		EventType:  "review.submitted",
		ReviewNo:   "REV-1",
		OrderNo:    "ORD-1",
		UserID:     7,
		ProductID:  21,
		SKUID:      101,
		Rating:     5,
		Content:    "good",
		OccurredAt: time.Date(2026, time.August, 24, 12, 0, 0, 123000000, time.UTC),
		Version:    1,
	}
}

func validBehaviorEvent() BehaviorEvent {
	return BehaviorEvent{
		EventID:      "22222222-2222-4222-8222-222222222222",
		EventType:    "product.viewed",
		UserID:       7,
		TraceID:      "trace-1",
		ResourceType: "product",
		ResourceID:   "1001",
		Payload:      []byte(`{"product_id":1001,"version":1}`),
		OccurredAt:   time.Date(2026, time.August, 24, 12, 1, 0, 123000000, time.UTC),
		Version:      1,
	}
}
