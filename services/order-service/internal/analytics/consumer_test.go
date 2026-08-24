package analytics

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/segmentio/kafka-go"
)

func TestReviewConsumerCommitsAfterHandlerSuccess(t *testing.T) {
	reader := &fakeReviewReader{message: kafka.Message{Topic: ReviewEventsTopic, Key: []byte("21"), Value: []byte(validReviewEventJSON(t))}}
	handler := &fakeReviewHandler{}
	consumer := newReviewConsumer(reader, handler, &fakeDeadLetterPublisher{}, time.Second)

	if err := consumer.handleMessage(context.Background(), reader.message); err != nil {
		t.Fatal(err)
	}
	if handler.calls != 1 || reader.commits != 1 {
		t.Fatalf("handler=%d commits=%d", handler.calls, reader.commits)
	}
}

func TestReviewConsumerDeadLettersMalformedMessageBeforeCommit(t *testing.T) {
	reader := &fakeReviewReader{message: kafka.Message{Topic: ReviewEventsTopic, Key: []byte("21"), Value: []byte(`{`)}}
	dlq := &fakeDeadLetterPublisher{}
	consumer := newReviewConsumer(reader, &fakeReviewHandler{}, dlq, time.Second)

	if err := consumer.handleMessage(context.Background(), reader.message); err != nil {
		t.Fatal(err)
	}
	if dlq.publishes != 1 || dlq.lastMessage.Topic != ReviewEventsDeadTopic || reader.commits != 1 {
		t.Fatalf("dlq=%d topic=%q commits=%d", dlq.publishes, dlq.lastMessage.Topic, reader.commits)
	}
}

func TestReviewConsumerDoesNotCommitHandlerFailure(t *testing.T) {
	reader := &fakeReviewReader{message: kafka.Message{Topic: ReviewEventsTopic, Key: []byte("21"), Value: []byte(validReviewEventJSON(t))}}
	handler := &fakeReviewHandler{err: errors.New("database unavailable")}
	consumer := newReviewConsumer(reader, handler, &fakeDeadLetterPublisher{}, time.Second)

	if err := consumer.handleMessage(context.Background(), reader.message); err == nil {
		t.Fatal("handleMessage() error = nil, want handler failure")
	}
	if reader.commits != 0 {
		t.Fatalf("commits=%d, want 0", reader.commits)
	}
}

func TestReviewConsumerDoesNotCommitDeadLetterFailure(t *testing.T) {
	reader := &fakeReviewReader{message: kafka.Message{Topic: ReviewEventsTopic, Key: []byte("21"), Value: []byte(`{`)}}
	dlq := &fakeDeadLetterPublisher{err: errors.New("dlq unavailable")}
	consumer := newReviewConsumer(reader, &fakeReviewHandler{}, dlq, time.Second)

	if err := consumer.handleMessage(context.Background(), reader.message); err == nil {
		t.Fatal("handleMessage() error = nil, want dlq failure")
	}
	if reader.commits != 0 {
		t.Fatalf("commits=%d, want 0", reader.commits)
	}
}

func TestBehaviorConsumerCommitsAfterHandlerSuccess(t *testing.T) {
	reader := &fakeReviewReader{message: kafka.Message{Topic: BehaviorEventsTopic, Key: []byte("7"), Value: []byte(validBehaviorEventJSON(t))}}
	handler := &fakeBehaviorHandler{}
	consumer := newBehaviorConsumer(reader, handler, &fakeDeadLetterPublisher{}, time.Second)

	if err := consumer.handleMessage(context.Background(), reader.message); err != nil {
		t.Fatal(err)
	}
	if handler.calls != 1 || reader.commits != 1 {
		t.Fatalf("handler=%d commits=%d", handler.calls, reader.commits)
	}
}

func TestBehaviorConsumerDeadLettersInvalidMessageBeforeCommit(t *testing.T) {
	reader := &fakeReviewReader{message: kafka.Message{Topic: BehaviorEventsTopic, Key: []byte("7"), Value: []byte(`{"event_id":"bad"}`)}}
	handler := &fakeBehaviorHandler{}
	dlq := &fakeDeadLetterPublisher{}
	consumer := newBehaviorConsumer(reader, handler, dlq, time.Second)

	if err := consumer.handleMessage(context.Background(), reader.message); err != nil {
		t.Fatal(err)
	}
	if handler.deadLetters != 1 || dlq.publishes != 1 || dlq.lastMessage.Topic != BehaviorEventsDeadTopic || reader.commits != 1 {
		t.Fatalf("dead=%d dlq=%d topic=%q commits=%d", handler.deadLetters, dlq.publishes, dlq.lastMessage.Topic, reader.commits)
	}
}

type fakeReviewReader struct {
	message kafka.Message
	commits int
}

func (r *fakeReviewReader) FetchMessage(context.Context) (kafka.Message, error) {
	return r.message, nil
}

func (r *fakeReviewReader) CommitMessages(context.Context, ...kafka.Message) error {
	r.commits++
	return nil
}

func (r *fakeReviewReader) Close() error { return nil }

type fakeDeadLetterPublisher struct {
	err         error
	publishes   int
	lastMessage kafka.Message
}

func (p *fakeDeadLetterPublisher) Publish(_ context.Context, message kafka.Message) error {
	p.publishes++
	p.lastMessage = message
	return p.err
}

func (p *fakeDeadLetterPublisher) Close() error { return nil }

type fakeReviewHandler struct {
	err   error
	calls int
}

func (h *fakeReviewHandler) HandleReviewEvent(context.Context, ReviewEvent) error {
	h.calls++
	return h.err
}

func validReviewEventJSON(t *testing.T) string {
	t.Helper()
	payload, err := json.Marshal(validReviewEvent())
	if err != nil {
		t.Fatal(err)
	}
	return string(payload)
}

type fakeBehaviorHandler struct {
	err         error
	calls       int
	deadLetters int
}

func (h *fakeBehaviorHandler) HandleBehaviorEvent(context.Context, BehaviorEvent) error {
	h.calls++
	return h.err
}

func (h *fakeBehaviorHandler) RecordDeadLetter(context.Context, DeadLetterRecord) error {
	h.deadLetters++
	return h.err
}

func validBehaviorEventJSON(t *testing.T) string {
	t.Helper()
	payload, err := json.Marshal(validBehaviorEvent())
	if err != nil {
		t.Fatal(err)
	}
	return string(payload)
}
