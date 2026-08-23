package knowledge

import (
	"context"
	"fmt"
	"strings"

	"github.com/yuyu945/AI-Shopping/internal/platform/apperror"
)

type EmbedConfig struct {
	Model     string
	Dimension int
}

type EmbedStore interface {
	StartConsumption(context.Context, string, string) (ConsumptionDecision, error)
	FindDocumentByNo(context.Context, string) (Document, error)
	ListDocumentChunks(context.Context, uint64) ([]Chunk, error)
	MarkEmbeddingTaskRetry(context.Context, string, uint64, uint32, string, string) error
	MarkDocumentReadyWithVectors(context.Context, string, Document, []ChunkVectorRef, string) error
	MarkConsumptionSucceeded(context.Context, string, string) error
}

type EmbedService struct {
	store       EmbedStore
	embedding   EmbeddingProvider
	vectorStore VectorStore
	config      EmbedConfig
}

func NewEmbedService(store EmbedStore, embedding EmbeddingProvider, vectorStore VectorStore, config EmbedConfig) *EmbedService {
	return &EmbedService{store: store, embedding: embedding, vectorStore: vectorStore, config: config}
}

func (s *EmbedService) HandleChunkEmbed(ctx context.Context, event ChunkEmbedEvent) error {
	if s == nil || s.store == nil || s.embedding == nil || s.vectorStore == nil || strings.TrimSpace(s.config.Model) == "" || s.config.Dimension <= 0 {
		return apperror.New(apperror.Internal, "knowledge embed service is unavailable")
	}
	if strings.TrimSpace(event.EventID) == "" || strings.TrimSpace(event.DocumentNo) == "" || event.PayloadVersion != 1 {
		return apperror.New(apperror.InvalidArgument, "invalid knowledge embed event")
	}
	decision, err := s.store.StartConsumption(ctx, event.EventID, chunkEmbedConsumerGroup)
	if err != nil {
		return apperror.Wrap(apperror.Internal, "knowledge embed consumption could not start", err)
	}
	if decision.AlreadySucceeded {
		return nil
	}
	document, err := s.store.FindDocumentByNo(ctx, event.DocumentNo)
	if err != nil {
		return apperror.Wrap(apperror.Internal, "knowledge document lookup failed", err)
	}
	if document.Status == DocumentReady {
		return s.store.MarkConsumptionSucceeded(ctx, event.EventID, chunkEmbedConsumerGroup)
	}
	chunks, err := s.store.ListDocumentChunks(ctx, document.ID)
	if err != nil {
		return apperror.Wrap(apperror.Internal, "knowledge chunks lookup failed", err)
	}
	texts := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		texts = append(texts, chunk.Content)
	}
	output, err := s.embedding.EmbedDocuments(ctx, EmbeddingInput{Model: strings.TrimSpace(s.config.Model), Texts: texts})
	if err != nil {
		_ = s.store.MarkEmbeddingTaskRetry(ctx, event.EventID, document.ID, document.Version, "EMBEDDING_FAILED", "embedding provider failed")
		return apperror.Wrap(apperror.Internal, "knowledge embedding failed", err)
	}
	if len(output.Vectors) != len(chunks) {
		_ = s.store.MarkEmbeddingTaskRetry(ctx, event.EventID, document.ID, document.Version, "EMBEDDING_FAILED", "embedding vector count mismatched")
		return apperror.New(apperror.Internal, "knowledge embedding failed")
	}
	upsertInput := VectorUpsertInput{Model: strings.TrimSpace(s.config.Model), Chunks: make([]VectorChunk, 0, len(chunks))}
	refs := make([]ChunkVectorRef, 0, len(chunks))
	for i, chunk := range chunks {
		vectorID := fmt.Sprintf("knowledge_chunk_%d", chunk.ID)
		upsertInput.Chunks = append(upsertInput.Chunks, VectorChunk{Chunk: chunk, Vector: output.Vectors[i], VectorID: vectorID})
		refs = append(refs, ChunkVectorRef{ChunkID: chunk.ID, VectorRef: vectorID})
	}
	if err := s.vectorStore.UpsertChunks(ctx, upsertInput); err != nil {
		_ = s.store.MarkEmbeddingTaskRetry(ctx, event.EventID, document.ID, document.Version, "VECTOR_UPSERT_FAILED", "vector upsert failed")
		return apperror.Wrap(apperror.Internal, "knowledge vector upsert failed", err)
	}
	if err := s.store.MarkDocumentReadyWithVectors(ctx, event.EventID, document, refs, strings.TrimSpace(s.config.Model)); err != nil {
		return apperror.Wrap(apperror.Internal, "knowledge document ready state could not be saved", err)
	}
	return s.store.MarkConsumptionSucceeded(ctx, event.EventID, chunkEmbedConsumerGroup)
}
