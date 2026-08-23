package knowledge

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/yuyu945/AI-Shopping/internal/platform/apperror"
)

func TestUploadDocumentStoresObjectAndCreatesPendingDocument(t *testing.T) {
	storage := &fakeStorage{}
	store := &fakeStore{nextVersion: 3}
	service := NewService(store, storage, fixedIDs("doc_1", "event-1"), fixedNow, Config{MaxFileBytes: 2 << 20, Bucket: "knowledge"})

	got, err := service.UploadDocument(context.Background(), UploadInput{
		UserID: 7, ProductID: 1001, DocType: DocFAQ, FileName: "faq.md",
		ContentType: "text/markdown", Content: []byte("# FAQ"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.DocumentNo != "doc_1" || got.Status != DocumentPending || got.Version != 3 {
		t.Fatalf("got=%#v", got)
	}
	if storage.putBucket != "knowledge" || !strings.Contains(storage.putKey, "knowledge/products/1001/FAQ/v3/") {
		t.Fatalf("bucket=%s key=%s", storage.putBucket, storage.putKey)
	}
	if storage.putContent != "# FAQ" {
		t.Fatalf("content=%q", storage.putContent)
	}
	if store.command.SourceHash == "" || len(store.command.SourceHash) != 64 {
		t.Fatalf("source_hash=%q", store.command.SourceHash)
	}
	if store.event.Topic != "knowledge.document.ingest" || store.event.EventKey != "doc_1" {
		t.Fatalf("event=%#v", store.event)
	}
}

func TestUploadDocumentValidatesInput(t *testing.T) {
	cases := []struct {
		name  string
		input UploadInput
	}{
		{name: "missing user", input: UploadInput{ProductID: 1, DocType: DocFAQ, FileName: "faq.md", ContentType: "text/markdown", Content: []byte("x")}},
		{name: "missing product", input: UploadInput{UserID: 1, DocType: DocFAQ, FileName: "faq.md", ContentType: "text/markdown", Content: []byte("x")}},
		{name: "invalid type", input: UploadInput{UserID: 1, ProductID: 1, DocType: DocType("GUIDE"), FileName: "faq.md", ContentType: "text/markdown", Content: []byte("x")}},
		{name: "empty content", input: UploadInput{UserID: 1, ProductID: 1, DocType: DocFAQ, FileName: "faq.md", ContentType: "text/markdown"}},
		{name: "oversized content", input: UploadInput{UserID: 1, ProductID: 1, DocType: DocFAQ, FileName: "faq.md", ContentType: "text/markdown", Content: []byte("too large")}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			service := NewService(&fakeStore{}, &fakeStorage{}, fixedIDs("doc_1", "event-1"), fixedNow, Config{MaxFileBytes: 4, Bucket: "knowledge"})
			if _, err := service.UploadDocument(context.Background(), tc.input); !hasAppCode(err, apperror.InvalidArgument) {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestUploadDocumentReturnsExistingDuplicateWithoutRewritingObject(t *testing.T) {
	existing := Document{ID: 9, DocumentNo: "doc_existing", ProductID: 1001, DocType: DocFAQ, Version: 2, Status: DocumentPending}
	store := &fakeStore{existing: existing}
	storage := &fakeStorage{}
	service := NewService(store, storage, fixedIDs("doc_1", "event-1"), fixedNow, Config{MaxFileBytes: 2 << 20, Bucket: "knowledge"})

	got, err := service.UploadDocument(context.Background(), UploadInput{
		UserID: 7, ProductID: 1001, DocType: DocFAQ, FileName: "faq.md",
		ContentType: "text/markdown", Content: []byte("# FAQ"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.DocumentNo != "doc_existing" {
		t.Fatalf("got=%#v", got)
	}
	if storage.putCount != 0 || store.createCount != 0 {
		t.Fatalf("put=%d create=%d", storage.putCount, store.createCount)
	}
}

func TestUploadDocumentCleansUpObjectWhenStoreFails(t *testing.T) {
	storeErr := errors.New("insert failed")
	storage := &fakeStorage{}
	service := NewService(&fakeStore{createErr: storeErr}, storage, fixedIDs("doc_1", "event-1"), fixedNow, Config{MaxFileBytes: 2 << 20, Bucket: "knowledge"})

	_, err := service.UploadDocument(context.Background(), UploadInput{
		UserID: 7, ProductID: 1001, DocType: DocFAQ, FileName: "faq.md",
		ContentType: "text/markdown", Content: []byte("# FAQ"),
	})
	if err == nil || !storage.deleted {
		t.Fatalf("err=%v deleted=%v", err, storage.deleted)
	}
}

type fakeStore struct {
	existing    Document
	createErr   error
	nextVersion uint32
	command     NewDocumentCommand
	event       OutboxEvent
	createCount int
}

func (s *fakeStore) FindDocumentBySourceHash(_ context.Context, productID uint64, docType DocType, sourceHash string) (Document, error) {
	if s.existing.DocumentNo != "" && s.existing.ProductID == productID && s.existing.DocType == docType && sourceHash != "" {
		return s.existing, nil
	}
	return Document{}, ErrDocumentNotFound
}

func (s *fakeStore) NextDocumentVersion(_ context.Context, productID uint64, docType DocType) (uint32, error) {
	if productID == 0 || docType == "" {
		return 0, errors.New("invalid version query")
	}
	if s.nextVersion == 0 {
		return 1, nil
	}
	return s.nextVersion, nil
}

func (s *fakeStore) CreateDocumentWithEvent(_ context.Context, command NewDocumentCommand, event OutboxEvent) (Document, error) {
	s.createCount++
	s.command = command
	s.event = event
	if s.createErr != nil {
		return Document{}, s.createErr
	}
	return Document{
		ID: 1, DocumentNo: command.DocumentNo, ProductID: command.ProductID, DocType: command.DocType,
		Version: command.Version, ObjectKey: command.ObjectKey, SourceHash: command.SourceHash,
		FileName: command.FileName, ContentType: command.ContentType, FileSizeBytes: command.FileSizeBytes,
		Status: DocumentPending, CreatedByUserID: command.CreatedByUserID, CreatedAt: fixedNow(), UpdatedAt: fixedNow(),
	}, nil
}

type fakeStorage struct {
	putBucket  string
	putKey     string
	putContent string
	putCount   int
	deleted    bool
}

func (s *fakeStorage) PutObject(_ context.Context, bucket, key string, content []byte, contentType string) error {
	s.putBucket = bucket
	s.putKey = key
	s.putContent = string(content)
	s.putCount++
	if contentType == "" {
		return errors.New("content type required")
	}
	return nil
}

func (s *fakeStorage) GetObject(context.Context, string, string) ([]byte, error) {
	return nil, errors.New("not implemented")
}

func (s *fakeStorage) DeleteObject(_ context.Context, bucket, key string) error {
	s.deleted = bucket != "" && key != ""
	return nil
}

func fixedIDs(values ...string) IDGenerator {
	index := 0
	return IDGeneratorFunc(func() string {
		if index >= len(values) {
			return ""
		}
		value := values[index]
		index++
		return value
	})
}

func fixedNow() time.Time {
	return time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
}

func hasAppCode(err error, code apperror.Code) bool {
	var appErr *apperror.Error
	return errors.As(err, &appErr) && appErr.Code == code
}
