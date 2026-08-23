package knowledge

import (
	"context"
	"errors"
	"testing"
)

func TestEmbedChunksMarksDocumentReadyAndCurrent(t *testing.T) {
	store := &fakeEmbedStore{document: embedDocument(), chunks: embedChunks()}
	provider := &fakeEmbeddingProvider{vectors: [][]float32{{0.1, 0.2}, {0.3, 0.4}}}
	vectorStore := &fakeVectorStore{}
	service := NewEmbedService(store, provider, vectorStore, EmbedConfig{Model: "text-embedding-3-small", Dimension: 1536})

	err := service.HandleChunkEmbed(context.Background(), ChunkEmbedEvent{EventID: "embed-event-1", DocumentNo: "doc_1", PayloadVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	if provider.input.Model != "text-embedding-3-small" || len(provider.input.Texts) != 2 {
		t.Fatalf("embedding input=%#v", provider.input)
	}
	if len(vectorStore.input.Chunks) != 2 {
		t.Fatalf("vector input=%#v", vectorStore.input)
	}
	if !store.ready || store.succeededEventID != "embed-event-1" {
		t.Fatalf("ready=%v succeeded=%q", store.ready, store.succeededEventID)
	}
}

func TestEmbedChunksDuplicateConsumptionSkipsWork(t *testing.T) {
	store := &fakeEmbedStore{decision: ConsumptionDecision{AlreadySucceeded: true}}
	service := NewEmbedService(store, &fakeEmbeddingProvider{}, &fakeVectorStore{}, EmbedConfig{Model: "text-embedding-3-small", Dimension: 1536})

	err := service.HandleChunkEmbed(context.Background(), ChunkEmbedEvent{EventID: "embed-event-1", DocumentNo: "doc_1", PayloadVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	if store.loadCalls != 0 {
		t.Fatalf("load calls = %d, want 0", store.loadCalls)
	}
}

func TestEmbedChunksEmbeddingFailureSchedulesRetry(t *testing.T) {
	store := &fakeEmbedStore{document: embedDocument(), chunks: embedChunks()}
	service := NewEmbedService(store, &fakeEmbeddingProvider{err: errors.New("provider down")}, &fakeVectorStore{}, EmbedConfig{Model: "text-embedding-3-small", Dimension: 1536})

	err := service.HandleChunkEmbed(context.Background(), ChunkEmbedEvent{EventID: "embed-event-1", DocumentNo: "doc_1", PayloadVersion: 1})
	if err == nil {
		t.Fatal("HandleChunkEmbed() error = nil, want embedding failure")
	}
	if store.retryCode != "EMBEDDING_FAILED" || store.ready {
		t.Fatalf("retry=%q ready=%v", store.retryCode, store.ready)
	}
}

func TestEmbedChunksVectorFailureDoesNotMarkReady(t *testing.T) {
	store := &fakeEmbedStore{document: embedDocument(), chunks: embedChunks()}
	service := NewEmbedService(store, &fakeEmbeddingProvider{vectors: [][]float32{{0.1}, {0.2}}}, &fakeVectorStore{err: errors.New("milvus down")}, EmbedConfig{Model: "text-embedding-3-small", Dimension: 1536})

	err := service.HandleChunkEmbed(context.Background(), ChunkEmbedEvent{EventID: "embed-event-1", DocumentNo: "doc_1", PayloadVersion: 1})
	if err == nil {
		t.Fatal("HandleChunkEmbed() error = nil, want vector failure")
	}
	if store.retryCode != "VECTOR_UPSERT_FAILED" || store.ready {
		t.Fatalf("retry=%q ready=%v", store.retryCode, store.ready)
	}
}

func TestFailedNewVersionLeavesPreviousReadyVersionCurrent(t *testing.T) {
	store := &fakeEmbedStore{document: embedDocument(), chunks: embedChunks(), previousCurrent: true}
	service := NewEmbedService(store, &fakeEmbeddingProvider{err: errors.New("provider down")}, &fakeVectorStore{}, EmbedConfig{Model: "text-embedding-3-small", Dimension: 1536})

	err := service.HandleChunkEmbed(context.Background(), ChunkEmbedEvent{EventID: "embed-event-1", DocumentNo: "doc_1", PayloadVersion: 1})
	if err == nil {
		t.Fatal("HandleChunkEmbed() error = nil, want embedding failure")
	}
	if !store.previousCurrent || store.ready {
		t.Fatalf("previousCurrent=%v ready=%v", store.previousCurrent, store.ready)
	}
}

func embedDocument() Document {
	return Document{ID: 123, DocumentNo: "doc_1", ProductID: 1001, DocType: DocFAQ, Version: 2, Status: DocumentProcessing}
}

func embedChunks() []Chunk {
	return []Chunk{
		{ID: 11, DocumentID: 123, ProductID: 1001, DocType: DocFAQ, Version: 2, ChunkIndex: 0, Content: "First", Status: ChunkPendingEmbedding},
		{ID: 12, DocumentID: 123, ProductID: 1001, DocType: DocFAQ, Version: 2, ChunkIndex: 1, Content: "Second", Status: ChunkPendingEmbedding},
	}
}

type fakeEmbedStore struct {
	decision         ConsumptionDecision
	document         Document
	chunks           []Chunk
	loadCalls        int
	ready            bool
	retryCode        string
	succeededEventID string
	previousCurrent  bool
}

func (s *fakeEmbedStore) StartConsumption(_ context.Context, _ string, consumerGroup string) (ConsumptionDecision, error) {
	if consumerGroup != chunkEmbedConsumerGroup {
		return ConsumptionDecision{}, errors.New("wrong consumer group")
	}
	return s.decision, nil
}

func (s *fakeEmbedStore) FindDocumentByNo(_ context.Context, documentNo string) (Document, error) {
	if s.document.DocumentNo != documentNo {
		return Document{}, ErrDocumentNotFound
	}
	return s.document, nil
}

func (s *fakeEmbedStore) ListDocumentChunks(context.Context, uint64) ([]Chunk, error) {
	s.loadCalls++
	return append([]Chunk(nil), s.chunks...), nil
}

func (s *fakeEmbedStore) MarkEmbeddingTaskRetry(_ context.Context, _ string, _ uint64, _ uint32, code, _ string) error {
	s.retryCode = code
	return nil
}

func (s *fakeEmbedStore) MarkDocumentReadyWithVectors(_ context.Context, _ string, _ Document, _ []ChunkVectorRef, _ string) error {
	s.ready = true
	s.previousCurrent = false
	return nil
}

func (s *fakeEmbedStore) MarkConsumptionSucceeded(_ context.Context, eventID, _ string) error {
	s.succeededEventID = eventID
	return nil
}

type fakeEmbeddingProvider struct {
	input   EmbeddingInput
	vectors [][]float32
	err     error
}

func (p *fakeEmbeddingProvider) EmbedDocuments(_ context.Context, input EmbeddingInput) (EmbeddingOutput, error) {
	p.input = input
	if p.err != nil {
		return EmbeddingOutput{}, p.err
	}
	return EmbeddingOutput{Vectors: p.vectors}, nil
}

type fakeVectorStore struct {
	input VectorUpsertInput
	err   error
}

func (s *fakeVectorStore) UpsertChunks(_ context.Context, input VectorUpsertInput) error {
	s.input = input
	return s.err
}

func (s *fakeVectorStore) Search(context.Context, VectorSearchInput) ([]VectorSearchHit, error) {
	return nil, errors.New("not implemented")
}
