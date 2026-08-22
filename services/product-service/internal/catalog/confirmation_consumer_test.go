package catalog

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/segmentio/kafka-go"
)

func TestConfirmationConsumerSkipsDuplicateEvent(t *testing.T) {
	store := &fakeConfirmationStore{}
	consumer := NewConfirmationConsumer(store, "product-inventory-confirmation")
	event := ConfirmationEvent{EventID: "event-1", ReservationID: "reservation-1", OrderNo: "order-1", PaymentAttemptID: "attempt-1", Version: 1}

	if err := consumer.Handle(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if err := consumer.Handle(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if store.calls != 2 {
		t.Fatalf("consume calls = %d, want 2", store.calls)
	}
}

func TestRetryConfirmationRetriesTransientFailure(t *testing.T) {
	calls := 0
	if err := retryConfirmation(context.Background(), func() error {
		calls++
		if calls < 3 {
			return errors.New("temporary database failure")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3", calls)
	}
}

func TestRetryConfirmationStopsAfterMaximumAttempts(t *testing.T) {
	calls := 0
	err := retryConfirmation(context.Background(), func() error {
		calls++
		return errors.New("database unavailable")
	})
	if err == nil {
		t.Fatal("retryConfirmation() error = nil, want exhausted retries")
	}
	if calls != confirmationRetryMaxAttempts {
		t.Fatalf("calls = %d, want %d", calls, confirmationRetryMaxAttempts)
	}
}

func TestRetryConfirmationStopsWhenContextIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := retryConfirmation(ctx, func() error { return errors.New("temporary database failure") })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context canceled", err)
	}
}

func TestKafkaConfirmationConsumerRetriesTransientFailureThenCommits(t *testing.T) {
	store := &fakeConfirmationStore{failures: 2}
	reader := &fakeConfirmationMessageReader{messages: []kafka.Message{{Value: confirmationEventJSON(t, "event-1")}}}
	consumer := newKafkaConfirmationConsumer(reader, NewConfirmationConsumer(store, ""), &fakeConfirmationDeadLetterPublisher{})
	ctx, cancel := context.WithCancel(context.Background())
	reader.onCommit = cancel

	err := consumer.Run(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context canceled", err)
	}
	if store.calls != 3 || reader.commits != 1 {
		t.Fatalf("store calls = %d, commits = %d; want 3, 1", store.calls, reader.commits)
	}
}

func TestKafkaConfirmationConsumerDeadLettersPermanentFailureThenProcessesNextMessage(t *testing.T) {
	store := &fakeConfirmationStore{failures: confirmationRetryMaxAttempts}
	reader := &fakeConfirmationMessageReader{messages: []kafka.Message{{Value: confirmationEventJSON(t, "event-1")}, {Value: confirmationEventJSON(t, "event-2")}}}
	dlq := &fakeConfirmationDeadLetterPublisher{}
	consumer := newKafkaConfirmationConsumer(reader, NewConfirmationConsumer(store, ""), dlq)
	ctx, cancel := context.WithCancel(context.Background())
	reader.onCommit = func() {
		if reader.commits == 2 {
			cancel()
		}
	}

	err := consumer.Run(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context canceled", err)
	}
	if len(dlq.messages) != 1 || reader.commits != 2 || store.calls != confirmationRetryMaxAttempts+1 {
		t.Fatalf("dlq=%d commits=%d store calls=%d", len(dlq.messages), reader.commits, store.calls)
	}
	assertDeadLetter(t, dlq.messages[0], "confirmation_retry_exhausted", confirmationEventJSON(t, "event-1"))
}

func TestKafkaConfirmationConsumerDeadLettersMalformedMessageAndContinues(t *testing.T) {
	raw := []byte("not-json")
	store := &fakeConfirmationStore{}
	reader := &fakeConfirmationMessageReader{messages: []kafka.Message{{Value: raw}, {Value: confirmationEventJSON(t, "event-2")}}}
	dlq := &fakeConfirmationDeadLetterPublisher{}
	consumer := newKafkaConfirmationConsumer(reader, NewConfirmationConsumer(store, ""), dlq)
	ctx, cancel := context.WithCancel(context.Background())
	reader.onCommit = func() {
		if reader.commits == 2 {
			cancel()
		}
	}

	err := consumer.Run(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context canceled", err)
	}
	if len(dlq.messages) != 1 || reader.commits != 2 || store.calls != 1 {
		t.Fatalf("dlq=%d commits=%d store calls=%d", len(dlq.messages), reader.commits, store.calls)
	}
	assertDeadLetter(t, dlq.messages[0], "malformed_confirmation_event", raw)
}

func TestKafkaConfirmationConsumerDoesNotCommitWhenDeadLetterPublicationFails(t *testing.T) {
	reader := &fakeConfirmationMessageReader{messages: []kafka.Message{{Value: []byte("not-json")}}}
	dlq := &fakeConfirmationDeadLetterPublisher{failures: confirmationRetryMaxAttempts}
	consumer := newKafkaConfirmationConsumer(reader, NewConfirmationConsumer(&fakeConfirmationStore{}, ""), dlq)

	err := consumer.Run(context.Background())
	if err == nil {
		t.Fatal("Run() error = nil, want dead-letter publication failure")
	}
	if reader.commits != 0 {
		t.Fatalf("commits = %d, want 0", reader.commits)
	}
	if len(dlq.messages) != confirmationRetryMaxAttempts {
		t.Fatalf("DLQ publishes = %d, want %d", len(dlq.messages), confirmationRetryMaxAttempts)
	}
}

func TestReservationRepositoryConfirmConsumedRollsBackWhenConfirmationFails(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Date(2026, time.August, 22, 9, 0, 0, 0, time.UTC)
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(insertReservationConsumptionQuery)).WithArgs("event-1", inventoryConfirmationConsumerGroup).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery(regexp.QuoteMeta(reservationRowsForUpdateQuery)).WithArgs("r-1").WillReturnRows(reservationRows(now.Add(time.Minute), ReservationReserved, []ReservationItem{{SKUID: 7, Quantity: 1}}))
	mock.ExpectExec(regexp.QuoteMeta(confirmReservationRowsQuery)).WithArgs(ReservationConfirmed, now, "r-1", ReservationReserved).WillReturnError(errors.New("database unavailable"))
	mock.ExpectRollback()
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(insertReservationConsumptionQuery)).WithArgs("event-1", inventoryConfirmationConsumerGroup).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery(regexp.QuoteMeta(reservationRowsForUpdateQuery)).WithArgs("r-1").WillReturnRows(reservationRows(now.Add(time.Minute), ReservationReserved, []ReservationItem{{SKUID: 7, Quantity: 1}}))
	mock.ExpectExec(regexp.QuoteMeta(confirmReservationRowsQuery)).WithArgs(ReservationConfirmed, now, "r-1", ReservationReserved).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err = NewReservationRepository(db).ConfirmConsumed(context.Background(), "event-1", inventoryConfirmationConsumerGroup, "r-1", now)
	if err == nil {
		t.Fatal("ConfirmConsumed() error = nil, want confirmation failure")
	}
	if err := NewReservationRepository(db).ConfirmConsumed(context.Background(), "event-1", inventoryConfirmationConsumerGroup, "r-1", now); err != nil {
		t.Fatalf("retry ConfirmConsumed() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestReservationRepositoryConfirmConsumedRollsBackWhenReservationIsUnknown(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(insertReservationConsumptionQuery)).WithArgs("event-1", inventoryConfirmationConsumerGroup).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery(regexp.QuoteMeta(reservationRowsForUpdateQuery)).WithArgs("missing").WillReturnRows(sqlmock.NewRows(reservationColumns))
	mock.ExpectRollback()

	err = NewReservationRepository(db).ConfirmConsumed(context.Background(), "event-1", inventoryConfirmationConsumerGroup, "missing", time.Now())
	if !errors.Is(err, ErrReservationNotFound) {
		t.Fatalf("error = %v, want unknown reservation", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestReservationRepositoryConfirmConsumedDuplicateSkipsSecondConfirmation(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(insertReservationConsumptionQuery)).WithArgs("event-1", inventoryConfirmationConsumerGroup).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	if err := NewReservationRepository(db).ConfirmConsumed(context.Background(), "event-1", inventoryConfirmationConsumerGroup, "r-1", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

type fakeConfirmationStore struct {
	calls    int
	failures int
}

func (s *fakeConfirmationStore) ConfirmConsumed(context.Context, string, string, string, time.Time) error {
	s.calls++
	if s.failures > 0 {
		s.failures--
		return errors.New("database unavailable")
	}
	return nil
}

type fakeConfirmationMessageReader struct {
	messages []kafka.Message
	commits  int
	onCommit func()
}

func (r *fakeConfirmationMessageReader) FetchMessage(ctx context.Context) (kafka.Message, error) {
	if len(r.messages) == 0 {
		<-ctx.Done()
		return kafka.Message{}, ctx.Err()
	}
	message := r.messages[0]
	r.messages = r.messages[1:]
	return message, nil
}

func (r *fakeConfirmationMessageReader) CommitMessages(context.Context, ...kafka.Message) error {
	r.commits++
	if r.onCommit != nil {
		r.onCommit()
	}
	return nil
}

func (r *fakeConfirmationMessageReader) Close() error { return nil }

type fakeConfirmationDeadLetterPublisher struct {
	messages []kafka.Message
	failures int
}

func (p *fakeConfirmationDeadLetterPublisher) Publish(_ context.Context, message kafka.Message) error {
	p.messages = append(p.messages, message)
	if p.failures > 0 {
		p.failures--
		return errors.New("kafka unavailable")
	}
	return nil
}

func (p *fakeConfirmationDeadLetterPublisher) Close() error { return nil }

func confirmationEventJSON(t *testing.T, eventID string) []byte {
	t.Helper()
	payload, err := json.Marshal(ConfirmationEvent{EventID: eventID, ReservationID: "reservation-1", OrderNo: "order-1", PaymentAttemptID: "attempt-1", Version: 1})
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

func assertDeadLetter(t *testing.T, message kafka.Message, reason string, raw []byte) {
	t.Helper()
	var payload struct {
		Reason         string `json:"reason"`
		RawEventBase64 string `json:"raw_event_base64"`
	}
	if err := json.Unmarshal(message.Value, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Reason != reason {
		t.Fatalf("reason = %q, want %q", payload.Reason, reason)
	}
	got, err := base64.StdEncoding.DecodeString(payload.RawEventBase64)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(raw) {
		t.Fatalf("raw event = %q, want %q", got, raw)
	}
}
