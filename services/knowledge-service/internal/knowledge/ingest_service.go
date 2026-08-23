package knowledge

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/yuyu945/AI-Shopping/internal/platform/apperror"
)

type IngestConfig struct {
	Bucket             string
	EmbeddingModel     string
	EmbeddingDimension int
	Chunker            Chunker
}

type IngestStore interface {
	StartConsumption(context.Context, string, string) (ConsumptionDecision, error)
	FindDocumentByNo(context.Context, string) (Document, error)
	MarkDocumentProcessing(context.Context, uint64) error
	SaveChunksAndEmbedEvent(context.Context, Document, []ChunkDraft, OutboxEvent) error
	MarkDocumentFailed(context.Context, uint64, string, string) error
	MarkConsumptionSucceeded(context.Context, string, string) error
}

type IngestService struct {
	store   IngestStore
	storage ObjectStorage
	ids     IDGenerator
	now     func() time.Time
	config  IngestConfig
}

func NewIngestService(store IngestStore, storage ObjectStorage, ids IDGenerator, now func() time.Time, config IngestConfig) *IngestService {
	if now == nil {
		now = time.Now
	}
	return &IngestService{store: store, storage: storage, ids: ids, now: now, config: config}
}

func (s *IngestService) HandleDocumentIngest(ctx context.Context, event IngestEvent) error {
	if s == nil || s.store == nil || s.storage == nil || s.ids == nil || strings.TrimSpace(s.config.Bucket) == "" {
		return apperror.New(apperror.Internal, "knowledge ingest service is unavailable")
	}
	if strings.TrimSpace(event.EventID) == "" || strings.TrimSpace(event.DocumentNo) == "" || event.PayloadVersion != 1 {
		return apperror.New(apperror.InvalidArgument, "invalid knowledge ingest event")
	}
	decision, err := s.store.StartConsumption(ctx, event.EventID, documentIngestConsumerGroup)
	if err != nil {
		return apperror.Wrap(apperror.Internal, "knowledge ingest consumption could not start", err)
	}
	if decision.AlreadySucceeded {
		return nil
	}
	document, err := s.store.FindDocumentByNo(ctx, event.DocumentNo)
	if err != nil {
		return apperror.Wrap(apperror.Internal, "knowledge document lookup failed", err)
	}
	if document.Status == DocumentReady {
		return s.store.MarkConsumptionSucceeded(ctx, event.EventID, documentIngestConsumerGroup)
	}
	if !supportedTextContentType(document.ContentType) {
		if err := s.store.MarkDocumentFailed(ctx, document.ID, "UNSUPPORTED_CONTENT_TYPE", "document content type is unsupported"); err != nil {
			return apperror.Wrap(apperror.Internal, "knowledge document failure could not be saved", err)
		}
		return s.store.MarkConsumptionSucceeded(ctx, event.EventID, documentIngestConsumerGroup)
	}
	if err := s.store.MarkDocumentProcessing(ctx, document.ID); err != nil {
		return apperror.Wrap(apperror.Internal, "knowledge document processing state could not be saved", err)
	}
	content, err := s.storage.GetObject(ctx, strings.TrimSpace(s.config.Bucket), document.ObjectKey)
	if err != nil {
		return dependencyError(ctx, "read document object", err)
	}
	chunks, err := s.config.Chunker.Split(document.FileName, content)
	if err != nil {
		if markErr := s.store.MarkDocumentFailed(ctx, document.ID, "EMPTY_CONTENT", "document content is empty"); markErr != nil {
			return apperror.Wrap(apperror.Internal, "knowledge document failure could not be saved", markErr)
		}
		return s.store.MarkConsumptionSucceeded(ctx, event.EventID, documentIngestConsumerGroup)
	}
	embedEvent, err := s.newChunkEmbedEvent(document, event.EventID, len(chunks))
	if err != nil {
		return err
	}
	if err := s.store.SaveChunksAndEmbedEvent(ctx, document, chunks, embedEvent); err != nil {
		return apperror.Wrap(apperror.Internal, "knowledge chunks could not be saved", err)
	}
	return s.store.MarkConsumptionSucceeded(ctx, event.EventID, documentIngestConsumerGroup)
}

type chunkEmbedPayload struct {
	EventID            string `json:"event_id"`
	EventType          string `json:"event_type"`
	DocumentNo         string `json:"document_no"`
	DocumentID         uint64 `json:"document_id"`
	ProductID          uint64 `json:"product_id"`
	DocType            string `json:"doc_type"`
	Version            uint32 `json:"version"`
	ChunkCount         int    `json:"chunk_count"`
	EmbeddingModel     string `json:"embedding_model"`
	EmbeddingDimension int    `json:"embedding_dimension"`
	PayloadVersion     int    `json:"payload_version"`
}

func (s *IngestService) newChunkEmbedEvent(document Document, ingestEventID string, chunkCount int) (OutboxEvent, error) {
	eventID := strings.TrimSpace(s.ids.New())
	if eventID == "" {
		return OutboxEvent{}, apperror.New(apperror.Internal, "knowledge ingest service is unavailable")
	}
	payload, err := json.Marshal(chunkEmbedPayload{
		EventID: eventID, EventType: chunkEmbedTopic, DocumentNo: document.DocumentNo, DocumentID: document.ID,
		ProductID: document.ProductID, DocType: string(document.DocType), Version: document.Version,
		ChunkCount: chunkCount, EmbeddingModel: strings.TrimSpace(s.config.EmbeddingModel),
		EmbeddingDimension: s.config.EmbeddingDimension, PayloadVersion: 1,
	})
	if err != nil {
		return OutboxEvent{}, apperror.Wrap(apperror.Internal, "chunk embed event could not be encoded", err)
	}
	return OutboxEvent{
		EventID: eventID, AggregateType: "KNOWLEDGE_DOCUMENT", AggregateID: document.DocumentNo,
		EventType: chunkEmbedTopic, Topic: chunkEmbedTopic, EventKey: document.DocumentNo, Payload: payload,
	}, nil
}

func supportedTextContentType(contentType string) bool {
	switch strings.ToLower(strings.TrimSpace(contentType)) {
	case "text/plain", "text/markdown", "application/json":
		return true
	default:
		return false
	}
}
