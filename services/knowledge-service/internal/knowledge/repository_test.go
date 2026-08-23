package knowledge

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestMySQLRepositoryNextDocumentVersion(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectQuery(regexp.QuoteMeta(queryNextDocumentVersion)).
		WithArgs(uint64(1001), DocFAQ).
		WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow(uint32(4)))

	got, err := NewMySQLRepository(db).NextDocumentVersion(context.Background(), 1001, DocFAQ)
	if err != nil || got != 4 {
		t.Fatalf("NextDocumentVersion() = %d, %v", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCreateDocumentWithEventCommitsDocumentAndOutboxTogether(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	command := newDocumentCommand()
	event := newOutboxEvent()
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(insertKnowledgeDocument)).
		WithArgs(command.DocumentNo, command.ProductID, command.DocType, command.Version, command.ObjectKey, command.SourceHash, command.FileName, command.ContentType, command.FileSizeBytes, command.CreatedByUserID, command.CreatedAt).
		WillReturnResult(sqlmock.NewResult(123, 1))
	mock.ExpectExec(regexp.QuoteMeta(insertKnowledgeOutbox)).
		WithArgs(event.EventID, event.AggregateType, event.AggregateID, event.EventType, event.Topic, event.EventKey, string(event.Payload)).
		WillReturnResult(sqlmock.NewResult(456, 1))
	mock.ExpectCommit()

	got, err := NewMySQLRepository(db).CreateDocumentWithEvent(context.Background(), command, event)
	if err != nil || got.ID != 123 || got.DocumentNo != command.DocumentNo || got.Version != command.Version || got.Status != DocumentPending {
		t.Fatalf("CreateDocumentWithEvent() = %#v, %v", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCreateDocumentWithEventRollsBackWhenOutboxInsertFails(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	command := newDocumentCommand()
	event := newOutboxEvent()
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(insertKnowledgeDocument)).
		WithArgs(command.DocumentNo, command.ProductID, command.DocType, command.Version, command.ObjectKey, command.SourceHash, command.FileName, command.ContentType, command.FileSizeBytes, command.CreatedByUserID, command.CreatedAt).
		WillReturnResult(sqlmock.NewResult(123, 1))
	mock.ExpectExec(regexp.QuoteMeta(insertKnowledgeOutbox)).
		WithArgs(event.EventID, event.AggregateType, event.AggregateID, event.EventType, event.Topic, event.EventKey, string(event.Payload)).
		WillReturnError(errors.New("outbox unavailable"))
	mock.ExpectRollback()

	if _, err := NewMySQLRepository(db).CreateDocumentWithEvent(context.Background(), command, event); err == nil {
		t.Fatal("CreateDocumentWithEvent error = nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestFindDocumentBySourceHashReturnsDocument(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectQuery(regexp.QuoteMeta(queryDocumentBySourceHash)).
		WithArgs(uint64(1001), DocFAQ, strings.Repeat("a", 64)).
		WillReturnRows(sqlmock.NewRows(documentColumns()).AddRow(uint64(123), "doc_1", uint64(1001), DocFAQ, uint32(2), "object-key", strings.Repeat("a", 64), "faq.md", "text/markdown", uint64(5), DocumentPending, uint64(7), fixedNow(), fixedNow()))

	got, err := NewMySQLRepository(db).FindDocumentBySourceHash(context.Background(), 1001, DocFAQ, strings.Repeat("a", 64))
	if err != nil || got.ID != 123 || got.Version != 2 || got.DocumentNo != "doc_1" {
		t.Fatalf("FindDocumentBySourceHash() = %#v, %v", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestStartConsumptionInsertsProcessingEvent(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectExec(regexp.QuoteMeta(insertKnowledgeConsumption)).
		WithArgs("event-1", documentIngestConsumerGroup).
		WillReturnResult(sqlmock.NewResult(1, 1))

	got, err := NewMySQLRepository(db).StartConsumption(context.Background(), "event-1", documentIngestConsumerGroup)
	if err != nil || got.AlreadySucceeded {
		t.Fatalf("StartConsumption() = %#v, %v", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestStartConsumptionSkipsSucceededDuplicate(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectExec(regexp.QuoteMeta(insertKnowledgeConsumption)).
		WithArgs("event-1", documentIngestConsumerGroup).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta(queryKnowledgeConsumptionStatus)).
		WithArgs("event-1", documentIngestConsumerGroup).
		WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("SUCCEEDED"))

	got, err := NewMySQLRepository(db).StartConsumption(context.Background(), "event-1", documentIngestConsumerGroup)
	if err != nil || !got.AlreadySucceeded {
		t.Fatalf("StartConsumption() = %#v, %v", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSaveChunksAndEmbedEventCommitsTogether(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	document := ingestDocument()
	chunks := []ChunkDraft{{ChunkIndex: 0, Section: "Battery", Content: "Lasts 10 hours.", ContentHash: strings.Repeat("b", 64)}}
	event := OutboxEvent{EventID: "embed-event-1", AggregateType: "KNOWLEDGE_DOCUMENT", AggregateID: "doc_1", EventType: chunkEmbedTopic, Topic: chunkEmbedTopic, EventKey: "doc_1", Payload: []byte(`{"event_id":"embed-event-1"}`)}

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(insertKnowledgeChunk)).
		WithArgs(document.ID, document.ProductID, document.DocType, document.Version, chunks[0].ChunkIndex, chunks[0].Section, nil, chunks[0].Content, chunks[0].ContentHash).
		WillReturnResult(sqlmock.NewResult(11, 1))
	mock.ExpectExec(regexp.QuoteMeta(insertKnowledgeOutbox)).
		WithArgs(event.EventID, event.AggregateType, event.AggregateID, event.EventType, event.Topic, event.EventKey, string(event.Payload)).
		WillReturnResult(sqlmock.NewResult(12, 1))
	mock.ExpectExec(regexp.QuoteMeta(updateKnowledgeDocumentChunkCount)).
		WithArgs(len(chunks), document.ID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if err := NewMySQLRepository(db).SaveChunksAndEmbedEvent(context.Background(), document, chunks, event); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func newDocumentCommand() NewDocumentCommand {
	return NewDocumentCommand{
		DocumentNo: "doc_1", ProductID: 1001, DocType: DocFAQ, Version: 2,
		ObjectKey: "knowledge/products/1001/FAQ/v2/hash-faq.md", SourceHash: strings.Repeat("a", 64),
		FileName: "faq.md", ContentType: "text/markdown", FileSizeBytes: 5, CreatedByUserID: 7,
		CreatedAt: fixedNow().UTC().Truncate(time.Millisecond),
	}
}

func newOutboxEvent() OutboxEvent {
	return OutboxEvent{
		EventID: "event-1", AggregateType: "KNOWLEDGE_DOCUMENT", AggregateID: "doc_1",
		EventType: documentIngestTopic, Topic: documentIngestTopic, EventKey: "doc_1",
		Payload: []byte(`{"event_id":"event-1"}`),
	}
}
