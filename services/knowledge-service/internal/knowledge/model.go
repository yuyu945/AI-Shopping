package knowledge

import (
	"context"
	"errors"
	"time"
)

const documentIngestTopic = "knowledge.document.ingest"
const chunkEmbedTopic = "knowledge.chunk.embed"

const documentIngestConsumerGroup = "knowledge-document-ingest-v1"
const chunkEmbedConsumerGroup = "knowledge-chunk-embed-v1"

var ErrDocumentNotFound = errors.New("document not found")

type DocType string

const (
	DocDetail    DocType = "DETAIL"
	DocSpec      DocType = "SPEC"
	DocFAQ       DocType = "FAQ"
	DocAfterSale DocType = "AFTER_SALE"
)

type DocumentStatus string

const (
	DocumentPending    DocumentStatus = "PENDING"
	DocumentProcessing DocumentStatus = "PROCESSING"
	DocumentReady      DocumentStatus = "READY"
	DocumentFailed     DocumentStatus = "FAILED"
)

type ChunkStatus string

const (
	ChunkPendingEmbedding ChunkStatus = "PENDING_EMBEDDING"
	ChunkEmbedded         ChunkStatus = "EMBEDDED"
	ChunkFailed           ChunkStatus = "FAILED"
)

type Document struct {
	ID              uint64
	DocumentNo      string
	ProductID       uint64
	DocType         DocType
	Version         uint32
	ObjectKey       string
	SourceHash      string
	FileName        string
	ContentType     string
	FileSizeBytes   uint64
	Status          DocumentStatus
	CreatedByUserID uint64
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type ChunkDraft struct {
	ChunkIndex  uint32
	Section     string
	SourcePage  *uint32
	Content     string
	ContentHash string
}

type Chunk struct {
	ID          uint64
	DocumentID  uint64
	ProductID   uint64
	DocType     DocType
	Version     uint32
	ChunkIndex  uint32
	Section     string
	SourcePage  *uint32
	Content     string
	ContentHash string
	VectorRef   string
	Status      ChunkStatus
}

type ChunkVectorRef struct {
	ChunkID   uint64
	VectorRef string
}

type UploadInput struct {
	UserID      uint64
	ProductID   uint64
	DocType     DocType
	FileName    string
	ContentType string
	Content     []byte
}

type NewDocumentCommand struct {
	DocumentNo      string
	ProductID       uint64
	DocType         DocType
	Version         uint32
	ObjectKey       string
	SourceHash      string
	FileName        string
	ContentType     string
	FileSizeBytes   uint64
	CreatedByUserID uint64
	CreatedAt       time.Time
}

type OutboxEvent struct {
	EventID       string
	AggregateType string
	AggregateID   string
	EventType     string
	Topic         string
	EventKey      string
	Payload       []byte
}

type IngestEvent struct {
	EventID        string `json:"event_id"`
	EventType      string `json:"event_type"`
	DocumentNo     string `json:"document_no"`
	DocumentID     uint64 `json:"document_id"`
	ProductID      uint64 `json:"product_id"`
	DocType        string `json:"doc_type"`
	Version        uint32 `json:"version"`
	ObjectKey      string `json:"object_key"`
	SourceHash     string `json:"source_hash"`
	PayloadVersion int    `json:"payload_version"`
}

type ChunkEmbedEvent struct {
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

type ConsumptionDecision struct {
	AlreadySucceeded bool
}

type EmbeddingInput struct {
	Model string
	Texts []string
}

type EmbeddingOutput struct {
	Vectors [][]float32
}

type VectorUpsertInput struct {
	Model  string
	Chunks []VectorChunk
}

type VectorChunk struct {
	Chunk    Chunk
	Vector   []float32
	VectorID string
}

type VectorSearchInput struct {
	ProductID uint64
	Query     []float32
	TopK      int
}

type VectorSearchHit struct {
	ChunkID uint64
	Score   float64
}

type EmbeddingProvider interface {
	EmbedDocuments(context.Context, EmbeddingInput) (EmbeddingOutput, error)
}

type VectorStore interface {
	UpsertChunks(context.Context, VectorUpsertInput) error
	Search(context.Context, VectorSearchInput) ([]VectorSearchHit, error)
}

type Store interface {
	FindDocumentBySourceHash(context.Context, uint64, DocType, string) (Document, error)
	NextDocumentVersion(context.Context, uint64, DocType) (uint32, error)
	CreateDocumentWithEvent(context.Context, NewDocumentCommand, OutboxEvent) (Document, error)
}

type ObjectStorage interface {
	PutObject(context.Context, string, string, []byte, string) error
	GetObject(context.Context, string, string) ([]byte, error)
	DeleteObject(context.Context, string, string) error
}

type IDGenerator interface {
	New() string
}

type IDGeneratorFunc func() string

func (f IDGeneratorFunc) New() string { return f() }

type Config struct {
	MaxFileBytes uint64
	Bucket       string
}
