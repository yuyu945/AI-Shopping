package knowledge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/yuyu945/AI-Shopping/internal/platform/apperror"
)

var safeFileNamePattern = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

type Service struct {
	store   Store
	storage ObjectStorage
	ids     IDGenerator
	now     func() time.Time
	config  Config
}

type documentOpsStore interface {
	ListDocuments(context.Context, DocumentListFilter) (DocumentListResult, error)
	GetDocumentDetail(context.Context, string) (DocumentDetail, error)
	RetryFailedDocument(context.Context, string, OutboxEvent) (Document, error)
}

func NewService(store Store, storage ObjectStorage, ids IDGenerator, now func() time.Time, config Config) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{store: store, storage: storage, ids: ids, now: now, config: config}
}

func (s *Service) UploadDocument(ctx context.Context, input UploadInput) (Document, error) {
	if s == nil || s.store == nil || s.storage == nil || s.ids == nil || strings.TrimSpace(s.config.Bucket) == "" || s.config.MaxFileBytes == 0 {
		return Document{}, apperror.New(apperror.Internal, "knowledge upload service is unavailable")
	}
	input, err := normalizeUpload(input, s.config.MaxFileBytes)
	if err != nil {
		return Document{}, err
	}
	sourceHash := hashContent(input.Content)
	if existing, err := s.store.FindDocumentBySourceHash(ctx, input.ProductID, input.DocType, sourceHash); err == nil {
		return existing, nil
	} else if !errors.Is(err, ErrDocumentNotFound) {
		return Document{}, apperror.Wrap(apperror.Internal, "document lookup failed", err)
	}
	documentNo := strings.TrimSpace(s.ids.New())
	eventID := strings.TrimSpace(s.ids.New())
	if documentNo == "" || eventID == "" {
		return Document{}, apperror.New(apperror.Internal, "knowledge upload service is unavailable")
	}
	version, err := s.store.NextDocumentVersion(ctx, input.ProductID, input.DocType)
	if err != nil {
		return Document{}, apperror.Wrap(apperror.Internal, "document version lookup failed", err)
	}
	if version == 0 {
		return Document{}, apperror.New(apperror.Internal, "document version is invalid")
	}
	objectKey := objectKey(input.ProductID, input.DocType, version, sourceHash, input.FileName)
	bucket := strings.TrimSpace(s.config.Bucket)
	if err := s.storage.PutObject(ctx, bucket, objectKey, append([]byte(nil), input.Content...), input.ContentType); err != nil {
		return Document{}, dependencyError(ctx, "store document object", err)
	}
	command := NewDocumentCommand{
		DocumentNo: documentNo, ProductID: input.ProductID, DocType: input.DocType, ObjectKey: objectKey,
		SourceHash: sourceHash, FileName: input.FileName, ContentType: input.ContentType,
		FileSizeBytes: uint64(len(input.Content)), Version: version, CreatedByUserID: input.UserID, CreatedAt: s.now().UTC().Truncate(time.Millisecond),
	}
	event, err := newIngestEvent(eventID, documentNo, command)
	if err != nil {
		_ = s.storage.DeleteObject(ctx, bucket, objectKey)
		return Document{}, err
	}
	document, err := s.store.CreateDocumentWithEvent(ctx, command, event)
	if err != nil {
		_ = s.storage.DeleteObject(ctx, bucket, objectKey)
		return Document{}, apperror.Wrap(apperror.Internal, "document upload could not be saved", err)
	}
	return cloneDocument(document), nil
}

func (s *Service) ListDocuments(ctx context.Context, filter DocumentListFilter) (DocumentListResult, error) {
	if s == nil {
		return DocumentListResult{}, apperror.New(apperror.Internal, "knowledge document service is unavailable")
	}
	store, ok := s.store.(documentOpsStore)
	if !ok {
		return DocumentListResult{}, apperror.New(apperror.Internal, "knowledge document service is unavailable")
	}
	filter = normalizeDocumentListFilter(filter)
	if filter.DocType != "" && !validDocType(filter.DocType) {
		return DocumentListResult{}, apperror.New(apperror.InvalidArgument, "doc_type is invalid")
	}
	if filter.Status != "" && !validDocumentStatus(filter.Status) {
		return DocumentListResult{}, apperror.New(apperror.InvalidArgument, "document status is invalid")
	}
	result, err := store.ListDocuments(ctx, filter)
	if err != nil {
		return DocumentListResult{}, apperror.Wrap(apperror.Internal, "document list could not be loaded", err)
	}
	return result, nil
}

func (s *Service) GetDocumentDetail(ctx context.Context, documentNo string) (DocumentDetail, error) {
	if s == nil {
		return DocumentDetail{}, apperror.New(apperror.Internal, "knowledge document service is unavailable")
	}
	store, ok := s.store.(documentOpsStore)
	if !ok {
		return DocumentDetail{}, apperror.New(apperror.Internal, "knowledge document service is unavailable")
	}
	documentNo = strings.TrimSpace(documentNo)
	if documentNo == "" {
		return DocumentDetail{}, apperror.New(apperror.InvalidArgument, "document_no is required")
	}
	detail, err := store.GetDocumentDetail(ctx, documentNo)
	if errors.Is(err, ErrDocumentNotFound) {
		return DocumentDetail{}, apperror.New(apperror.NotFound, "document not found")
	}
	if err != nil {
		return DocumentDetail{}, apperror.Wrap(apperror.Internal, "document detail could not be loaded", err)
	}
	return detail, nil
}

func (s *Service) RetryDocument(ctx context.Context, documentNo string) (Document, error) {
	if s == nil || s.ids == nil {
		return Document{}, apperror.New(apperror.Internal, "knowledge retry service is unavailable")
	}
	store, ok := s.store.(documentOpsStore)
	if !ok {
		return Document{}, apperror.New(apperror.Internal, "knowledge retry service is unavailable")
	}
	detail, err := s.GetDocumentDetail(ctx, documentNo)
	if err != nil {
		return Document{}, err
	}
	if detail.Document.Status != DocumentFailed {
		return Document{}, apperror.New(apperror.InvalidArgument, "only failed documents can be retried")
	}
	eventID := strings.TrimSpace(s.ids.New())
	if eventID == "" {
		return Document{}, apperror.New(apperror.Internal, "knowledge retry service is unavailable")
	}
	event, err := newIngestEvent(eventID, detail.Document.DocumentNo, commandFromDocument(detail.Document))
	if err != nil {
		return Document{}, err
	}
	document, err := store.RetryFailedDocument(ctx, detail.Document.DocumentNo, event)
	if errors.Is(err, ErrDocumentRetryNotAllowed) {
		return Document{}, apperror.New(apperror.InvalidArgument, "only failed documents can be retried")
	}
	if err != nil {
		return Document{}, apperror.Wrap(apperror.Internal, "document retry could not be saved", err)
	}
	return document, nil
}

func normalizeUpload(input UploadInput, maxBytes uint64) (UploadInput, error) {
	input.FileName = sanitizeFileName(input.FileName)
	input.ContentType = strings.TrimSpace(input.ContentType)
	if input.UserID == 0 || input.ProductID == 0 {
		return UploadInput{}, apperror.New(apperror.InvalidArgument, "user_id and product_id are required")
	}
	if !validDocType(input.DocType) {
		return UploadInput{}, apperror.New(apperror.InvalidArgument, "doc_type is invalid")
	}
	if input.FileName == "" || len(input.FileName) > 255 || input.ContentType == "" || len(input.ContentType) > 128 {
		return UploadInput{}, apperror.New(apperror.InvalidArgument, "file metadata is invalid")
	}
	if len(input.Content) == 0 || uint64(len(input.Content)) > maxBytes {
		return UploadInput{}, apperror.New(apperror.InvalidArgument, "file size is invalid")
	}
	return input, nil
}

func validDocType(value DocType) bool {
	switch value {
	case DocDetail, DocSpec, DocFAQ, DocAfterSale:
		return true
	default:
		return false
	}
}

func validDocumentStatus(value DocumentStatus) bool {
	switch value {
	case DocumentPending, DocumentProcessing, DocumentReady, DocumentFailed:
		return true
	default:
		return false
	}
}

func normalizeDocumentListFilter(filter DocumentListFilter) DocumentListFilter {
	if filter.PageSize == 0 || filter.PageSize > 100 {
		filter.PageSize = 50
	}
	filter.PageToken = strings.TrimSpace(filter.PageToken)
	return filter
}

func sanitizeFileName(value string) string {
	name := filepath.Base(strings.TrimSpace(value))
	name = safeFileNamePattern.ReplaceAllString(name, "_")
	return strings.Trim(name, "._-")
}

func hashContent(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func objectKey(productID uint64, docType DocType, version uint32, sourceHash, fileName string) string {
	return fmt.Sprintf("knowledge/products/%d/%s/v%d/%s-%s", productID, docType, version, sourceHash, fileName)
}

func newIngestEvent(eventID, documentNo string, command NewDocumentCommand) (OutboxEvent, error) {
	payload, err := json.Marshal(struct {
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
	}{
		EventID: eventID, EventType: documentIngestTopic, DocumentNo: documentNo, ProductID: command.ProductID,
		DocType: string(command.DocType), Version: command.Version, ObjectKey: command.ObjectKey, SourceHash: command.SourceHash, PayloadVersion: 1,
	})
	if err != nil {
		return OutboxEvent{}, apperror.Wrap(apperror.Internal, "document event could not be encoded", err)
	}
	return OutboxEvent{
		EventID: eventID, AggregateType: "KNOWLEDGE_DOCUMENT", AggregateID: documentNo,
		EventType: documentIngestTopic, Topic: documentIngestTopic, EventKey: documentNo, Payload: payload,
	}, nil
}

func commandFromDocument(document Document) NewDocumentCommand {
	return NewDocumentCommand{
		DocumentNo: document.DocumentNo, ProductID: document.ProductID, DocType: document.DocType, Version: document.Version,
		ObjectKey: document.ObjectKey, SourceHash: document.SourceHash, FileName: document.FileName, ContentType: document.ContentType,
		FileSizeBytes: document.FileSizeBytes, CreatedByUserID: document.CreatedByUserID, CreatedAt: document.CreatedAt,
	}
}

func dependencyError(ctx context.Context, message string, err error) error {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return apperror.Wrap(apperror.DependencyTimeout, "knowledge dependency timed out", err)
	}
	return apperror.Wrap(apperror.Internal, message+" failed", err)
}

func cloneDocument(document Document) Document {
	return document
}
