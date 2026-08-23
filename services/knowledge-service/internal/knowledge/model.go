package knowledge

import (
	"context"
	"errors"
	"time"
)

const documentIngestTopic = "knowledge.document.ingest"

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

type Store interface {
	FindDocumentBySourceHash(context.Context, uint64, DocType, string) (Document, error)
	NextDocumentVersion(context.Context, uint64, DocType) (uint32, error)
	CreateDocumentWithEvent(context.Context, NewDocumentCommand, OutboxEvent) (Document, error)
}

type ObjectStorage interface {
	PutObject(context.Context, string, string, []byte, string) error
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
