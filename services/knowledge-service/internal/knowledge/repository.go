package knowledge

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/go-sql-driver/mysql"
)

const queryNextDocumentVersion = `SELECT COALESCE(MAX(version), 0) + 1 FROM knowledge_documents WHERE product_id = ? AND doc_type = ?`
const queryDocumentBySourceHash = `SELECT id, document_no, product_id, doc_type, version, object_key, source_hash, file_name, content_type, file_size_bytes, status, created_by_user_id, created_at, updated_at FROM knowledge_documents WHERE product_id = ? AND doc_type = ? AND source_hash = ?`
const insertKnowledgeDocument = `INSERT INTO knowledge_documents (document_no, product_id, doc_type, version, object_key, source_hash, file_name, content_type, file_size_bytes, status, created_by_user_id, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'PENDING', ?, ?)`
const insertKnowledgeOutbox = `INSERT INTO outbox_events (event_id, aggregate_type, aggregate_id, event_type, topic, event_key, payload) VALUES (?, ?, ?, ?, ?, ?, CAST(? AS JSON))`

type MySQLRepository struct {
	db *sql.DB
}

func NewMySQLRepository(db *sql.DB) *MySQLRepository {
	return &MySQLRepository{db: db}
}

func (r *MySQLRepository) NextDocumentVersion(ctx context.Context, productID uint64, docType DocType) (uint32, error) {
	var version uint32
	if err := r.db.QueryRowContext(ctx, queryNextDocumentVersion, productID, docType).Scan(&version); err != nil {
		return 0, fmt.Errorf("read next document version: %w", err)
	}
	return version, nil
}

func (r *MySQLRepository) FindDocumentBySourceHash(ctx context.Context, productID uint64, docType DocType, sourceHash string) (Document, error) {
	document, err := scanDocument(r.db.QueryRowContext(ctx, queryDocumentBySourceHash, productID, docType, sourceHash))
	if errors.Is(err, sql.ErrNoRows) {
		return Document{}, ErrDocumentNotFound
	}
	if err != nil {
		return Document{}, fmt.Errorf("read document by source hash: %w", err)
	}
	return document, nil
}

func (r *MySQLRepository) CreateDocumentWithEvent(ctx context.Context, command NewDocumentCommand, event OutboxEvent) (document Document, err error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Document{}, fmt.Errorf("begin knowledge upload transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	result, err := tx.ExecContext(ctx, insertKnowledgeDocument, command.DocumentNo, command.ProductID, command.DocType, command.Version, command.ObjectKey, command.SourceHash, command.FileName, command.ContentType, command.FileSizeBytes, command.CreatedByUserID, command.CreatedAt)
	if err != nil {
		if isDuplicate(err) {
			_ = tx.Rollback()
			return r.FindDocumentBySourceHash(ctx, command.ProductID, command.DocType, command.SourceHash)
		}
		return Document{}, fmt.Errorf("insert knowledge document: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return Document{}, fmt.Errorf("read knowledge document id: %w", err)
	}
	if _, err = tx.ExecContext(ctx, insertKnowledgeOutbox, event.EventID, event.AggregateType, event.AggregateID, event.EventType, event.Topic, event.EventKey, string(event.Payload)); err != nil {
		return Document{}, fmt.Errorf("insert knowledge outbox event: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return Document{}, fmt.Errorf("commit knowledge upload transaction: %w", err)
	}
	return Document{
		ID: uint64(id), DocumentNo: command.DocumentNo, ProductID: command.ProductID, DocType: command.DocType,
		Version: command.Version, ObjectKey: command.ObjectKey, SourceHash: command.SourceHash,
		FileName: command.FileName, ContentType: command.ContentType, FileSizeBytes: command.FileSizeBytes,
		Status: DocumentPending, CreatedByUserID: command.CreatedByUserID, CreatedAt: command.CreatedAt, UpdatedAt: command.CreatedAt,
	}, nil
}

type scanner interface {
	Scan(...any) error
}

func scanDocument(row scanner) (Document, error) {
	var document Document
	if err := row.Scan(
		&document.ID, &document.DocumentNo, &document.ProductID, &document.DocType, &document.Version,
		&document.ObjectKey, &document.SourceHash, &document.FileName, &document.ContentType, &document.FileSizeBytes,
		&document.Status, &document.CreatedByUserID, &document.CreatedAt, &document.UpdatedAt,
	); err != nil {
		return Document{}, err
	}
	return document, nil
}

func documentColumns() []string {
	return []string{"id", "document_no", "product_id", "doc_type", "version", "object_key", "source_hash", "file_name", "content_type", "file_size_bytes", "status", "created_by_user_id", "created_at", "updated_at"}
}

func isDuplicate(err error) bool {
	var mysqlErr *mysql.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1062
}
