package knowledge

import (
	"context"
	"errors"
	"testing"

	"github.com/yuyu945/AI-Shopping/internal/platform/apperror"
)

func TestSearchProductKnowledgeReturnsOnlyCurrentReadyChunks(t *testing.T) {
	store := &fakeRetrievalStore{
		current:  []Document{{ID: 123, DocumentNo: "doc_1", ProductID: 1001, DocType: DocFAQ, Version: 2, Status: DocumentReady}},
		snippets: []KnowledgeSnippet{{ChunkID: 11, DocumentNo: "doc_1", ProductID: 1001, DocType: DocFAQ, Version: 2, Content: "Battery lasts 10 hours.", Score: 0.91}},
	}
	provider := &fakeEmbeddingProvider{vectors: [][]float32{{0.1, 0.2}}}
	vectorStore := &fakeVectorStore{searchHits: []VectorSearchHit{{ChunkID: 11, Score: 0.91}, {ChunkID: 99, Score: 0.80}}}
	service := NewRetrievalService(store, provider, vectorStore, RetrievalConfig{Model: "text-embedding-3-small", DefaultTopK: 5, MaxTopK: 10})

	got, err := service.SearchProductKnowledge(context.Background(), SearchKnowledgeInput{ProductID: 1001, Query: "battery life", DocTypes: []DocType{DocFAQ}, TopK: 5})
	if err != nil {
		t.Fatal(err)
	}
	if got.FallbackReason != "" || len(got.Snippets) != 1 || got.Snippets[0].ChunkID != 11 {
		t.Fatalf("result=%#v", got)
	}
	if vectorStore.searchInput.TopK != 5 || len(vectorStore.searchInput.Filters) != 1 {
		t.Fatalf("vector search input=%#v", vectorStore.searchInput)
	}
}

func TestSearchProductKnowledgeReturnsFallbackWhenNoReadyKnowledge(t *testing.T) {
	service := NewRetrievalService(&fakeRetrievalStore{}, &fakeEmbeddingProvider{}, &fakeVectorStore{}, RetrievalConfig{Model: "text-embedding-3-small", DefaultTopK: 5, MaxTopK: 10})

	got, err := service.SearchProductKnowledge(context.Background(), SearchKnowledgeInput{ProductID: 1001, Query: "battery life"})
	if err != nil {
		t.Fatal(err)
	}
	if got.FallbackReason != FallbackReasonNoReadyKnowledge || len(got.Snippets) != 0 {
		t.Fatalf("result=%#v", got)
	}
}

func TestSearchProductKnowledgeCapsTopK(t *testing.T) {
	store := &fakeRetrievalStore{current: []Document{{ID: 123, DocumentNo: "doc_1", ProductID: 1001, DocType: DocFAQ, Version: 2, Status: DocumentReady}}}
	provider := &fakeEmbeddingProvider{vectors: [][]float32{{0.1}}}
	vectorStore := &fakeVectorStore{}
	service := NewRetrievalService(store, provider, vectorStore, RetrievalConfig{Model: "text-embedding-3-small", DefaultTopK: 5, MaxTopK: 10})

	_, err := service.SearchProductKnowledge(context.Background(), SearchKnowledgeInput{ProductID: 1001, Query: "battery life", TopK: 50})
	if err != nil {
		t.Fatal(err)
	}
	if vectorStore.searchInput.TopK != 10 {
		t.Fatalf("topK = %d, want 10", vectorStore.searchInput.TopK)
	}
}

func TestSearchProductKnowledgeRejectsEmptyQuery(t *testing.T) {
	service := NewRetrievalService(&fakeRetrievalStore{}, &fakeEmbeddingProvider{}, &fakeVectorStore{}, RetrievalConfig{Model: "text-embedding-3-small", DefaultTopK: 5, MaxTopK: 10})

	if _, err := service.SearchProductKnowledge(context.Background(), SearchKnowledgeInput{ProductID: 1001, Query: " \n "}); !hasAppCode(err, apperror.InvalidArgument) {
		t.Fatalf("err=%v", err)
	}
}

func TestSearchProductKnowledgeMapsVectorTimeout(t *testing.T) {
	store := &fakeRetrievalStore{current: []Document{{ID: 123, DocumentNo: "doc_1", ProductID: 1001, DocType: DocFAQ, Version: 2, Status: DocumentReady}}}
	provider := &fakeEmbeddingProvider{vectors: [][]float32{{0.1}}}
	vectorStore := &fakeVectorStore{searchErr: context.DeadlineExceeded}
	service := NewRetrievalService(store, provider, vectorStore, RetrievalConfig{Model: "text-embedding-3-small", DefaultTopK: 5, MaxTopK: 10})

	if _, err := service.SearchProductKnowledge(context.Background(), SearchKnowledgeInput{ProductID: 1001, Query: "battery life"}); !hasAppCode(err, apperror.DependencyTimeout) {
		t.Fatalf("err=%v", err)
	}
}

type fakeRetrievalStore struct {
	current  []Document
	snippets []KnowledgeSnippet
}

func (s *fakeRetrievalStore) ListCurrentReadyDocuments(_ context.Context, productID uint64, _ []DocType) ([]Document, error) {
	if productID == 0 {
		return nil, errors.New("product id required")
	}
	return append([]Document(nil), s.current...), nil
}

func (s *fakeRetrievalStore) HydrateKnowledgeSnippets(_ context.Context, _ uint64, _ []VectorSearchHit) ([]KnowledgeSnippet, error) {
	return append([]KnowledgeSnippet(nil), s.snippets...), nil
}
