package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestIngestDocumentPersistsChunksAndPublishesEmbedEvent(t *testing.T) {
	store := &fakeIngestStore{document: ingestDocument()}
	storage := &fakeIngestStorage{content: []byte("# Battery\n\nLasts 10 hours.")}
	service := NewIngestService(store, storage, fixedIDs("embed-event-1"), fixedNow, IngestConfig{
		Bucket:             "knowledge",
		EmbeddingModel:     "text-embedding-3-small",
		EmbeddingDimension: 1536,
		Chunker:            Chunker{TargetChars: 900, MaxChars: 1200},
	})

	err := service.HandleDocumentIngest(context.Background(), IngestEvent{EventID: "ingest-event-1", DocumentNo: "doc_1", PayloadVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	if store.processingID != 123 || store.succeededEventID != "ingest-event-1" {
		t.Fatalf("processing=%d succeeded=%q", store.processingID, store.succeededEventID)
	}
	if len(store.chunks) != 1 || store.chunks[0].Section != "Battery" {
		t.Fatalf("chunks=%#v", store.chunks)
	}
	if store.event.Topic != chunkEmbedTopic || store.event.EventKey != "doc_1" {
		t.Fatalf("event=%#v", store.event)
	}
	var payload chunkEmbedPayload
	if err := json.Unmarshal(store.event.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.EmbeddingModel != "text-embedding-3-small" || payload.EmbeddingDimension != 1536 || payload.ChunkCount != 1 {
		t.Fatalf("payload=%#v", payload)
	}
}

func TestIngestDocumentDuplicateConsumptionSkipsWork(t *testing.T) {
	store := &fakeIngestStore{decision: ConsumptionDecision{AlreadySucceeded: true}}
	service := NewIngestService(store, &fakeIngestStorage{}, fixedIDs("embed-event-1"), fixedNow, IngestConfig{Bucket: "knowledge", EmbeddingModel: "text-embedding-3-small", EmbeddingDimension: 1536})

	err := service.HandleDocumentIngest(context.Background(), IngestEvent{EventID: "ingest-event-1", DocumentNo: "doc_1", PayloadVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	if store.findCalls != 0 {
		t.Fatalf("find calls = %d, want 0", store.findCalls)
	}
}

func TestIngestDocumentUnsupportedContentTypeMarksFailed(t *testing.T) {
	store := &fakeIngestStore{document: func() Document {
		document := ingestDocument()
		document.ContentType = "application/pdf"
		return document
	}()}
	service := NewIngestService(store, &fakeIngestStorage{}, fixedIDs("embed-event-1"), fixedNow, IngestConfig{Bucket: "knowledge", EmbeddingModel: "text-embedding-3-small", EmbeddingDimension: 1536})

	err := service.HandleDocumentIngest(context.Background(), IngestEvent{EventID: "ingest-event-1", DocumentNo: "doc_1", PayloadVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	if store.failedCode != "UNSUPPORTED_CONTENT_TYPE" || store.savedChunks {
		t.Fatalf("failedCode=%q saved=%v", store.failedCode, store.savedChunks)
	}
}

func TestIngestDocumentReusesExistingChunksOnReplay(t *testing.T) {
	store := &fakeIngestStore{document: ingestDocument()}
	storage := &fakeIngestStorage{content: []byte("# Battery\n\nLasts 10 hours.")}
	service := NewIngestService(store, storage, fixedIDs("embed-event-1", "embed-event-2"), fixedNow, IngestConfig{
		Bucket:             "knowledge",
		EmbeddingModel:     "text-embedding-3-small",
		EmbeddingDimension: 1536,
		Chunker:            Chunker{TargetChars: 900, MaxChars: 1200},
	})

	if err := service.HandleDocumentIngest(context.Background(), IngestEvent{EventID: "ingest-event-1", DocumentNo: "doc_1", PayloadVersion: 1}); err != nil {
		t.Fatal(err)
	}
	if err := service.HandleDocumentIngest(context.Background(), IngestEvent{EventID: "ingest-event-1", DocumentNo: "doc_1", PayloadVersion: 1}); err != nil {
		t.Fatal(err)
	}
	if store.saveCalls != 1 {
		t.Fatalf("save calls = %d, want 1", store.saveCalls)
	}
}

func ingestDocument() Document {
	return Document{
		ID: 123, DocumentNo: "doc_1", ProductID: 1001, DocType: DocFAQ, Version: 2,
		ObjectKey: "knowledge/products/1001/FAQ/v2/hash-faq.md", SourceHash: "hash",
		FileName: "faq.md", ContentType: "text/markdown", Status: DocumentPending,
	}
}

type fakeIngestStore struct {
	decision         ConsumptionDecision
	document         Document
	findCalls        int
	processingID     uint64
	chunks           []ChunkDraft
	event            OutboxEvent
	succeededEventID string
	failedCode       string
	savedChunks      bool
	saveCalls        int
	seenEvents       map[string]bool
}

func (s *fakeIngestStore) StartConsumption(_ context.Context, eventID, consumerGroup string) (ConsumptionDecision, error) {
	if consumerGroup != documentIngestConsumerGroup {
		return ConsumptionDecision{}, errors.New("wrong consumer group")
	}
	if s.decision.AlreadySucceeded {
		return s.decision, nil
	}
	if s.seenEvents == nil {
		s.seenEvents = make(map[string]bool)
	}
	if s.seenEvents[eventID] {
		return ConsumptionDecision{AlreadySucceeded: true}, nil
	}
	s.seenEvents[eventID] = true
	return ConsumptionDecision{}, nil
}

func (s *fakeIngestStore) FindDocumentByNo(_ context.Context, documentNo string) (Document, error) {
	s.findCalls++
	if s.document.DocumentNo != documentNo {
		return Document{}, ErrDocumentNotFound
	}
	return s.document, nil
}

func (s *fakeIngestStore) MarkDocumentProcessing(_ context.Context, documentID uint64) error {
	s.processingID = documentID
	return nil
}

func (s *fakeIngestStore) SaveChunksAndEmbedEvent(_ context.Context, _ Document, chunks []ChunkDraft, event OutboxEvent) error {
	s.saveCalls++
	s.savedChunks = true
	s.chunks = append([]ChunkDraft(nil), chunks...)
	s.event = event
	return nil
}

func (s *fakeIngestStore) MarkDocumentFailed(_ context.Context, _ uint64, code, _ string) error {
	s.failedCode = code
	return nil
}

func (s *fakeIngestStore) MarkConsumptionSucceeded(_ context.Context, eventID, _ string) error {
	s.succeededEventID = eventID
	return nil
}

type fakeIngestStorage struct {
	content []byte
}

func (s *fakeIngestStorage) PutObject(context.Context, string, string, []byte, string) error {
	return errors.New("not implemented")
}

func (s *fakeIngestStorage) GetObject(_ context.Context, bucket, key string) ([]byte, error) {
	if bucket == "" || key == "" {
		return nil, errors.New("object identity is required")
	}
	return append([]byte(nil), s.content...), nil
}

func (s *fakeIngestStorage) DeleteObject(context.Context, string, string) error {
	return errors.New("not implemented")
}
