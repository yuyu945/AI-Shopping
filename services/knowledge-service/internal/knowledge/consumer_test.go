package knowledge

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"

	"github.com/segmentio/kafka-go"
)

func TestKafkaKnowledgeConsumerHandlesAndCommitsMessage(t *testing.T) {
	handler := &fakeKnowledgeEventHandler{}
	reader := &fakeKnowledgeReader{messages: []kafka.Message{{Topic: documentIngestTopic, Key: []byte("doc_1"), Value: mustJSON(t, IngestEvent{EventID: "event-1", DocumentNo: "doc_1", PayloadVersion: 1})}}}
	consumer := newKafkaKnowledgeConsumer(reader, handler, &fakeKnowledgeDLQ{})
	ctx, cancel := context.WithCancel(context.Background())
	reader.onCommit = cancel

	err := consumer.Run(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context canceled", err)
	}
	if handler.ingestCalls != 1 || reader.commits != 1 {
		t.Fatalf("ingestCalls=%d commits=%d", handler.ingestCalls, reader.commits)
	}
}

func TestKafkaKnowledgeConsumerDeadLettersMalformedMessage(t *testing.T) {
	reader := &fakeKnowledgeReader{messages: []kafka.Message{{Topic: documentIngestTopic, Key: []byte("doc_1"), Value: []byte("not-json")}}}
	dlq := &fakeKnowledgeDLQ{}
	consumer := newKafkaKnowledgeConsumer(reader, &fakeKnowledgeEventHandler{}, dlq)
	ctx, cancel := context.WithCancel(context.Background())
	reader.onCommit = cancel

	err := consumer.Run(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context canceled", err)
	}
	if reader.commits != 1 || len(dlq.messages) != 1 {
		t.Fatalf("commits=%d dlq=%d", reader.commits, len(dlq.messages))
	}
	assertKnowledgeDeadLetter(t, dlq.messages[0], "malformed_knowledge_event", []byte("not-json"))
}

func TestKafkaKnowledgeConsumerDoesNotCommitWhenHandlerFails(t *testing.T) {
	reader := &fakeKnowledgeReader{messages: []kafka.Message{{Topic: chunkEmbedTopic, Key: []byte("doc_1"), Value: mustJSON(t, ChunkEmbedEvent{EventID: "event-1", DocumentNo: "doc_1", PayloadVersion: 1})}}}
	handler := &fakeKnowledgeEventHandler{err: errors.New("db down")}
	consumer := newKafkaKnowledgeConsumer(reader, handler, &fakeKnowledgeDLQ{})

	err := consumer.Run(context.Background())
	if err == nil {
		t.Fatal("Run() error = nil, want handler failure")
	}
	if reader.commits != 0 || handler.embedCalls != 1 {
		t.Fatalf("commits=%d embedCalls=%d", reader.commits, handler.embedCalls)
	}
}

type fakeKnowledgeEventHandler struct {
	ingestCalls int
	embedCalls  int
	err         error
}

func (h *fakeKnowledgeEventHandler) HandleDocumentIngest(context.Context, IngestEvent) error {
	h.ingestCalls++
	return h.err
}

func (h *fakeKnowledgeEventHandler) HandleChunkEmbed(context.Context, ChunkEmbedEvent) error {
	h.embedCalls++
	return h.err
}

type fakeKnowledgeReader struct {
	messages []kafka.Message
	commits  int
	onCommit func()
}

func (r *fakeKnowledgeReader) FetchMessage(ctx context.Context) (kafka.Message, error) {
	if len(r.messages) == 0 {
		<-ctx.Done()
		return kafka.Message{}, ctx.Err()
	}
	message := r.messages[0]
	r.messages = r.messages[1:]
	return message, nil
}

func (r *fakeKnowledgeReader) CommitMessages(context.Context, ...kafka.Message) error {
	r.commits++
	if r.onCommit != nil {
		r.onCommit()
	}
	return nil
}

func (r *fakeKnowledgeReader) Close() error { return nil }

type fakeKnowledgeDLQ struct {
	messages []kafka.Message
}

func (d *fakeKnowledgeDLQ) Publish(_ context.Context, message kafka.Message) error {
	d.messages = append(d.messages, message)
	return nil
}

func (d *fakeKnowledgeDLQ) Close() error { return nil }

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func assertKnowledgeDeadLetter(t *testing.T, message kafka.Message, reason string, raw []byte) {
	t.Helper()
	var payload struct {
		Reason         string `json:"reason"`
		RawEventBase64 string `json:"raw_event_base64"`
	}
	if err := json.Unmarshal(message.Value, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Reason != reason {
		t.Fatalf("reason=%q want=%q", payload.Reason, reason)
	}
	got, err := base64.StdEncoding.DecodeString(payload.RawEventBase64)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(raw) {
		t.Fatalf("raw=%q want=%q", got, raw)
	}
}
