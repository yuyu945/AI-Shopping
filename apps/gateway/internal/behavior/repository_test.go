package behavior

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestMySQLRepositoryRecordsBehaviorOutboxEvent(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	event := Event{
		EventID: "33333333-3333-4333-8333-333333333333", UserID: 7, EventType: "product.viewed",
		TraceID: "trace-1", ResourceType: "product", ResourceID: "1001",
		Payload: []byte(`{"product_id":1001,"version":1}`), OccurredAt: time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC),
	}

	mock.ExpectExec(regexp.QuoteMeta(insertBehaviorOutbox)).
		WithArgs(event.EventID, event.UserID, event.EventType, BehaviorEventsTopic, "7", string(event.Payload)).
		WillReturnResult(sqlmock.NewResult(1, 1))

	if err := NewMySQLRepository(db).Record(context.Background(), event); err != nil {
		t.Fatalf("Record() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMySQLRepositoryRejectsPIIBehaviorPayload(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	event := Event{EventID: "33333333-3333-4333-8333-333333333333", UserID: 7, EventType: "product.viewed", TraceID: "trace-1", ResourceType: "product", ResourceID: "1001", Payload: []byte(`{"phone":"13800000000"}`)}

	if err := NewMySQLRepository(db).Record(context.Background(), event); err == nil {
		t.Fatal("Record() error = nil, want invalid payload")
	}
}
