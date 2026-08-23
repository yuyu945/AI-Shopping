package knowledge

import (
	"context"
	"errors"
	"strings"

	"github.com/yuyu945/AI-Shopping/internal/platform/apperror"
)

const FallbackReasonNoReadyKnowledge = "NO_READY_KNOWLEDGE"

type RetrievalConfig struct {
	Model       string
	DefaultTopK int
	MaxTopK     int
}

type RetrievalStore interface {
	ListCurrentReadyDocuments(context.Context, uint64, []DocType) ([]Document, error)
	HydrateKnowledgeSnippets(context.Context, uint64, []VectorSearchHit) ([]KnowledgeSnippet, error)
}

type RetrievalService struct {
	store       RetrievalStore
	embedding   EmbeddingProvider
	vectorStore VectorStore
	config      RetrievalConfig
}

func NewRetrievalService(store RetrievalStore, embedding EmbeddingProvider, vectorStore VectorStore, config RetrievalConfig) *RetrievalService {
	return &RetrievalService{store: store, embedding: embedding, vectorStore: vectorStore, config: config}
}

func (s *RetrievalService) SearchProductKnowledge(ctx context.Context, input SearchKnowledgeInput) (SearchKnowledgeResult, error) {
	if s == nil || s.store == nil || s.embedding == nil || s.vectorStore == nil || strings.TrimSpace(s.config.Model) == "" {
		return SearchKnowledgeResult{}, apperror.New(apperror.Internal, "knowledge retrieval service is unavailable")
	}
	input.Query = strings.TrimSpace(input.Query)
	if input.ProductID == 0 || input.Query == "" {
		return SearchKnowledgeResult{}, apperror.New(apperror.InvalidArgument, "product_id and query are required")
	}
	topK := s.topK(input.TopK)
	documents, err := s.store.ListCurrentReadyDocuments(ctx, input.ProductID, input.DocTypes)
	if err != nil {
		return SearchKnowledgeResult{}, apperror.Wrap(apperror.Internal, "current knowledge lookup failed", err)
	}
	if len(documents) == 0 {
		return SearchKnowledgeResult{FallbackReason: FallbackReasonNoReadyKnowledge}, nil
	}
	embedding, err := s.embedding.EmbedDocuments(ctx, EmbeddingInput{Model: strings.TrimSpace(s.config.Model), Texts: []string{input.Query}})
	if err != nil {
		return SearchKnowledgeResult{}, retrievalDependencyError(ctx, "knowledge query embedding failed", err)
	}
	if len(embedding.Vectors) != 1 {
		return SearchKnowledgeResult{}, apperror.New(apperror.Internal, "knowledge query embedding failed")
	}
	hits, err := s.vectorStore.Search(ctx, VectorSearchInput{ProductID: input.ProductID, Query: embedding.Vectors[0], TopK: topK, Filters: vectorFilters(documents)})
	if err != nil {
		return SearchKnowledgeResult{}, retrievalDependencyError(ctx, "knowledge vector search failed", err)
	}
	snippets, err := s.store.HydrateKnowledgeSnippets(ctx, input.ProductID, hits)
	if err != nil {
		return SearchKnowledgeResult{}, apperror.Wrap(apperror.Internal, "knowledge snippets lookup failed", err)
	}
	return SearchKnowledgeResult{Snippets: snippets}, nil
}

func (s *RetrievalService) topK(value int) int {
	if value <= 0 {
		value = s.config.DefaultTopK
	}
	if value <= 0 {
		value = 5
	}
	max := s.config.MaxTopK
	if max <= 0 {
		max = 10
	}
	if value > max {
		value = max
	}
	return value
}

func vectorFilters(documents []Document) []VectorDocumentFilter {
	filters := make([]VectorDocumentFilter, 0, len(documents))
	for _, document := range documents {
		filters = append(filters, VectorDocumentFilter{DocumentID: document.ID, DocType: document.DocType, Version: document.Version})
	}
	return filters
}

func retrievalDependencyError(ctx context.Context, message string, err error) error {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return apperror.Wrap(apperror.DependencyTimeout, "knowledge dependency timed out", err)
	}
	return apperror.Wrap(apperror.Internal, message, err)
}
