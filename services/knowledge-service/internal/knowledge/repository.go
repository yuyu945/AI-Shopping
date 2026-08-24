package knowledge

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/go-sql-driver/mysql"
)

const queryNextDocumentVersion = `SELECT COALESCE(MAX(version), 0) + 1 FROM knowledge_documents WHERE product_id = ? AND doc_type = ?`
const queryDocumentBySourceHash = `SELECT id, document_no, product_id, doc_type, version, object_key, source_hash, file_name, content_type, file_size_bytes, status, created_by_user_id, created_at, updated_at FROM knowledge_documents WHERE product_id = ? AND doc_type = ? AND source_hash = ?`
const queryDocumentByNo = `SELECT id, document_no, product_id, doc_type, version, object_key, source_hash, file_name, content_type, file_size_bytes, status, created_by_user_id, created_at, updated_at FROM knowledge_documents WHERE document_no = ?`
const queryDocumentDetailByNo = `SELECT id, document_no, product_id, doc_type, version, object_key, source_hash, file_name, content_type, file_size_bytes, embedding_model, status, chunk_count, is_current_ready, ready_at, error_code, error_message, created_by_user_id, created_at, processed_at, updated_at FROM knowledge_documents WHERE document_no = ?`
const queryDocumentDetailByNoForUpdate = queryDocumentDetailByNo + ` FOR UPDATE`
const insertKnowledgeDocument = `INSERT INTO knowledge_documents (document_no, product_id, doc_type, version, object_key, source_hash, file_name, content_type, file_size_bytes, status, created_by_user_id, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'PENDING', ?, ?)`
const insertKnowledgeOutbox = `INSERT INTO outbox_events (event_id, aggregate_type, aggregate_id, event_type, topic, event_key, payload) VALUES (?, ?, ?, ?, ?, ?, CAST(? AS JSON))`
const insertKnowledgeConsumption = `INSERT IGNORE INTO event_consumptions (event_id, consumer_group, status) VALUES (?, ?, 'PROCESSING')`
const queryKnowledgeConsumptionStatus = `SELECT status FROM event_consumptions WHERE event_id = ? AND consumer_group = ?`
const updateKnowledgeConsumptionSucceeded = `UPDATE event_consumptions SET status = 'SUCCEEDED', consumed_at = CURRENT_TIMESTAMP(3) WHERE event_id = ? AND consumer_group = ?`
const updateKnowledgeDocumentProcessing = `UPDATE knowledge_documents SET status = 'PROCESSING', error_code = NULL, error_message = NULL WHERE id = ? AND status IN ('PENDING','PROCESSING')`
const updateKnowledgeDocumentFailed = `UPDATE knowledge_documents SET status = 'FAILED', error_code = ?, error_message = ?, processed_at = CURRENT_TIMESTAMP(3), is_current_ready = 0 WHERE id = ?`
const updateKnowledgeDocumentRetryPending = `UPDATE knowledge_documents SET status = 'PENDING', error_code = NULL, error_message = NULL, processed_at = NULL WHERE id = ? AND status = 'FAILED'`
const insertKnowledgeChunk = `INSERT INTO knowledge_chunks (document_id, product_id, doc_type, version, chunk_index, section, source_page, content, content_hash, status) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'PENDING_EMBEDDING') ON DUPLICATE KEY UPDATE section = VALUES(section), source_page = VALUES(source_page), content = VALUES(content), content_hash = VALUES(content_hash), status = IF(status = 'EMBEDDED', status, VALUES(status))`
const updateKnowledgeDocumentChunkCount = `UPDATE knowledge_documents SET chunk_count = ? WHERE id = ?`
const queryDocumentChunks = `SELECT id, document_id, product_id, doc_type, version, chunk_index, section, source_page, content, content_hash, vector_ref, status FROM knowledge_chunks WHERE document_id = ? ORDER BY chunk_index ASC`
const upsertEmbeddingTaskRetry = `INSERT INTO embedding_tasks (event_id, document_id, version, status, retry_count, last_error) VALUES (?, ?, ?, 'FAILED', 1, ?) ON DUPLICATE KEY UPDATE status = 'FAILED', retry_count = retry_count + 1, last_error = VALUES(last_error), updated_at = CURRENT_TIMESTAMP(3)`
const updateKnowledgeChunkEmbedded = `UPDATE knowledge_chunks SET status = 'EMBEDDED', vector_ref = ? WHERE id = ? AND document_id = ?`
const upsertEmbeddingTaskDone = `INSERT INTO embedding_tasks (event_id, document_id, version, status) VALUES (?, ?, ?, 'DONE') ON DUPLICATE KEY UPDATE status = 'DONE', last_error = NULL, updated_at = CURRENT_TIMESTAMP(3)`
const clearCurrentReadyDocuments = `UPDATE knowledge_documents SET is_current_ready = 0 WHERE product_id = ? AND doc_type = ? AND id <> ? AND is_current_ready = 1`
const markKnowledgeDocumentReady = `UPDATE knowledge_documents SET status = 'READY', is_current_ready = 1, ready_at = CURRENT_TIMESTAMP(3), embedding_model = ?, chunk_count = ?, processed_at = CURRENT_TIMESTAMP(3), error_code = NULL, error_message = NULL WHERE id = ?`

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

func (r *MySQLRepository) FindDocumentByNo(ctx context.Context, documentNo string) (Document, error) {
	document, err := scanDocument(r.db.QueryRowContext(ctx, queryDocumentByNo, documentNo))
	if errors.Is(err, sql.ErrNoRows) {
		return Document{}, ErrDocumentNotFound
	}
	if err != nil {
		return Document{}, fmt.Errorf("read document by number: %w", err)
	}
	return document, nil
}

func (r *MySQLRepository) ListDocuments(ctx context.Context, filter DocumentListFilter) (DocumentListResult, error) {
	filter = normalizeDocumentListFilter(filter)
	args := listDocumentArgs(filter)
	rows, err := r.db.QueryContext(ctx, queryListDocuments(filter), args...)
	if err != nil {
		return DocumentListResult{}, fmt.Errorf("read knowledge documents: %w", err)
	}
	defer rows.Close()
	documents := make([]Document, 0)
	for rows.Next() {
		document, err := scanOpsDocument(rows)
		if err != nil {
			return DocumentListResult{}, err
		}
		documents = append(documents, document)
	}
	if err := rows.Err(); err != nil {
		return DocumentListResult{}, fmt.Errorf("read knowledge document rows: %w", err)
	}
	return DocumentListResult{Documents: documents}, nil
}

func (r *MySQLRepository) GetDocumentDetail(ctx context.Context, documentNo string) (DocumentDetail, error) {
	document, err := scanOpsDocument(r.db.QueryRowContext(ctx, queryDocumentDetailByNo, documentNo))
	if errors.Is(err, sql.ErrNoRows) {
		return DocumentDetail{}, ErrDocumentNotFound
	}
	if err != nil {
		return DocumentDetail{}, fmt.Errorf("read knowledge document detail: %w", err)
	}
	chunks, err := r.ListDocumentChunks(ctx, document.ID)
	if err != nil {
		return DocumentDetail{}, err
	}
	return DocumentDetail{Document: document, Chunks: chunks}, nil
}

func (r *MySQLRepository) RetryFailedDocument(ctx context.Context, documentNo string, event OutboxEvent) (document Document, err error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Document{}, fmt.Errorf("begin knowledge retry transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	document, err = scanOpsDocument(tx.QueryRowContext(ctx, queryDocumentDetailByNoForUpdate, documentNo))
	if errors.Is(err, sql.ErrNoRows) {
		return Document{}, ErrDocumentNotFound
	}
	if err != nil {
		return Document{}, fmt.Errorf("read failed knowledge document: %w", err)
	}
	if document.Status != DocumentFailed {
		return Document{}, ErrDocumentRetryNotAllowed
	}
	result, err := tx.ExecContext(ctx, updateKnowledgeDocumentRetryPending, document.ID)
	if err = requireSingleRow(result, err, "mark knowledge document retry pending"); err != nil {
		return Document{}, err
	}
	if _, err = tx.ExecContext(ctx, insertKnowledgeOutbox, event.EventID, event.AggregateType, event.AggregateID, event.EventType, event.Topic, event.EventKey, string(event.Payload)); err != nil {
		return Document{}, fmt.Errorf("insert retry knowledge outbox event: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return Document{}, fmt.Errorf("commit knowledge retry transaction: %w", err)
	}
	document.Status = DocumentPending
	document.ErrorCode = ""
	document.ErrorMessage = ""
	document.ProcessedAt = nil
	return document, nil
}

func (r *MySQLRepository) StartConsumption(ctx context.Context, eventID, consumerGroup string) (ConsumptionDecision, error) {
	result, err := r.db.ExecContext(ctx, insertKnowledgeConsumption, eventID, consumerGroup)
	if err != nil {
		return ConsumptionDecision{}, fmt.Errorf("insert event consumption: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return ConsumptionDecision{}, fmt.Errorf("read event consumption insert count: %w", err)
	}
	if rows == 1 {
		return ConsumptionDecision{}, nil
	}
	var status string
	if err := r.db.QueryRowContext(ctx, queryKnowledgeConsumptionStatus, eventID, consumerGroup).Scan(&status); err != nil {
		return ConsumptionDecision{}, fmt.Errorf("read event consumption status: %w", err)
	}
	if status == "SUCCEEDED" {
		return ConsumptionDecision{AlreadySucceeded: true}, nil
	}
	return ConsumptionDecision{}, errors.New("event consumption is already processing")
}

func (r *MySQLRepository) MarkConsumptionSucceeded(ctx context.Context, eventID, consumerGroup string) error {
	result, err := r.db.ExecContext(ctx, updateKnowledgeConsumptionSucceeded, eventID, consumerGroup)
	return requireSingleRow(result, err, "mark event consumption succeeded")
}

func (r *MySQLRepository) MarkDocumentProcessing(ctx context.Context, documentID uint64) error {
	result, err := r.db.ExecContext(ctx, updateKnowledgeDocumentProcessing, documentID)
	return requireSingleRow(result, err, "mark document processing")
}

func (r *MySQLRepository) MarkDocumentFailed(ctx context.Context, documentID uint64, code, message string) error {
	result, err := r.db.ExecContext(ctx, updateKnowledgeDocumentFailed, code, message, documentID)
	return requireSingleRow(result, err, "mark document failed")
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

func (r *MySQLRepository) SaveChunksAndEmbedEvent(ctx context.Context, document Document, chunks []ChunkDraft, event OutboxEvent) (err error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin knowledge chunk transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	for _, chunk := range chunks {
		if _, err = tx.ExecContext(ctx, insertKnowledgeChunk, document.ID, document.ProductID, document.DocType, document.Version, chunk.ChunkIndex, chunk.Section, sourcePageValue(chunk.SourcePage), chunk.Content, chunk.ContentHash); err != nil {
			return fmt.Errorf("insert knowledge chunk: %w", err)
		}
	}
	if _, err = tx.ExecContext(ctx, insertKnowledgeOutbox, event.EventID, event.AggregateType, event.AggregateID, event.EventType, event.Topic, event.EventKey, string(event.Payload)); err != nil {
		if !isDuplicate(err) {
			return fmt.Errorf("insert chunk embed outbox event: %w", err)
		}
	}
	if _, err = tx.ExecContext(ctx, updateKnowledgeDocumentChunkCount, len(chunks), document.ID); err != nil {
		return fmt.Errorf("update knowledge document chunk count: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit knowledge chunk transaction: %w", err)
	}
	return nil
}

func (r *MySQLRepository) ListDocumentChunks(ctx context.Context, documentID uint64) ([]Chunk, error) {
	rows, err := r.db.QueryContext(ctx, queryDocumentChunks, documentID)
	if err != nil {
		return nil, fmt.Errorf("read document chunks: %w", err)
	}
	defer rows.Close()

	chunks := make([]Chunk, 0)
	for rows.Next() {
		chunk, err := scanChunk(rows)
		if err != nil {
			return nil, err
		}
		chunks = append(chunks, chunk)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read document chunks rows: %w", err)
	}
	return chunks, nil
}

func (r *MySQLRepository) MarkEmbeddingTaskRetry(ctx context.Context, eventID string, documentID uint64, version uint32, code, message string) error {
	lastError := code
	if message != "" {
		lastError = code
	}
	_, err := r.db.ExecContext(ctx, upsertEmbeddingTaskRetry, eventID, documentID, version, lastError)
	if err != nil {
		return fmt.Errorf("upsert embedding task retry: %w", err)
	}
	return nil
}

func (r *MySQLRepository) MarkDocumentReadyWithVectors(ctx context.Context, eventID string, document Document, refs []ChunkVectorRef, model string) (err error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin knowledge ready transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	for _, ref := range refs {
		result, execErr := tx.ExecContext(ctx, updateKnowledgeChunkEmbedded, ref.VectorRef, ref.ChunkID, document.ID)
		if execErr != nil {
			return fmt.Errorf("mark knowledge chunk embedded: %w", execErr)
		}
		if execErr = requireSingleRow(result, nil, "mark knowledge chunk embedded"); execErr != nil {
			return execErr
		}
	}
	if _, err = tx.ExecContext(ctx, upsertEmbeddingTaskDone, eventID, document.ID, document.Version); err != nil {
		return fmt.Errorf("upsert embedding task done: %w", err)
	}
	if _, err = tx.ExecContext(ctx, clearCurrentReadyDocuments, document.ProductID, document.DocType, document.ID); err != nil {
		return fmt.Errorf("clear current knowledge documents: %w", err)
	}
	result, err := tx.ExecContext(ctx, markKnowledgeDocumentReady, model, len(refs), document.ID)
	if err != nil {
		return fmt.Errorf("mark knowledge document ready: %w", err)
	}
	if err = requireSingleRow(result, nil, "mark knowledge document ready"); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit knowledge ready transaction: %w", err)
	}
	return nil
}

func (r *MySQLRepository) ListCurrentReadyDocuments(ctx context.Context, productID uint64, docTypes []DocType) ([]Document, error) {
	if len(docTypes) == 0 {
		docTypes = []DocType{DocDetail, DocSpec, DocFAQ, DocAfterSale}
	}
	args := make([]any, 0, 1+len(docTypes))
	args = append(args, productID)
	for _, docType := range docTypes {
		args = append(args, docType)
	}
	rows, err := r.db.QueryContext(ctx, queryCurrentReadyDocuments(len(docTypes)), args...)
	if err != nil {
		return nil, fmt.Errorf("read current ready documents: %w", err)
	}
	defer rows.Close()

	documents := make([]Document, 0)
	for rows.Next() {
		document, err := scanDocument(rows)
		if err != nil {
			return nil, err
		}
		documents = append(documents, document)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read current ready document rows: %w", err)
	}
	return documents, nil
}

func (r *MySQLRepository) HydrateKnowledgeSnippets(ctx context.Context, productID uint64, hits []VectorSearchHit) ([]KnowledgeSnippet, error) {
	if len(hits) == 0 {
		return nil, nil
	}
	args := make([]any, 0, 1+len(hits))
	args = append(args, productID)
	scores := make(map[uint64]float64, len(hits))
	order := make([]uint64, 0, len(hits))
	for _, hit := range hits {
		args = append(args, hit.ChunkID)
		scores[hit.ChunkID] = hit.Score
		order = append(order, hit.ChunkID)
	}
	rows, err := r.db.QueryContext(ctx, queryKnowledgeSnippetsByChunkIDs(len(hits)), args...)
	if err != nil {
		return nil, fmt.Errorf("read knowledge snippets: %w", err)
	}
	defer rows.Close()

	byChunkID := make(map[uint64]KnowledgeSnippet, len(hits))
	for rows.Next() {
		snippet, err := scanKnowledgeSnippet(rows)
		if err != nil {
			return nil, err
		}
		snippet.Score = scores[snippet.ChunkID]
		byChunkID[snippet.ChunkID] = snippet
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read knowledge snippet rows: %w", err)
	}
	snippets := make([]KnowledgeSnippet, 0, len(byChunkID))
	for _, chunkID := range order {
		if snippet, ok := byChunkID[chunkID]; ok {
			snippets = append(snippets, snippet)
		}
	}
	return snippets, nil
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

func documentOpsColumns() []string {
	return []string{"id", "document_no", "product_id", "doc_type", "version", "object_key", "source_hash", "file_name", "content_type", "file_size_bytes", "embedding_model", "status", "chunk_count", "is_current_ready", "ready_at", "error_code", "error_message", "created_by_user_id", "created_at", "processed_at", "updated_at"}
}

func chunkColumns() []string {
	return []string{"id", "document_id", "product_id", "doc_type", "version", "chunk_index", "section", "source_page", "content", "content_hash", "vector_ref", "status"}
}

func queryCurrentReadyDocuments(docTypeCount int) string {
	return `SELECT id, document_no, product_id, doc_type, version, object_key, source_hash, file_name, content_type, file_size_bytes, status, created_by_user_id, created_at, updated_at FROM knowledge_documents WHERE product_id = ? AND doc_type IN (` + placeholders(docTypeCount) + `) AND status = 'READY' AND is_current_ready = 1 ORDER BY doc_type ASC, version DESC`
}

func queryListDocuments(filter DocumentListFilter) string {
	clauses := []string{"1 = 1"}
	if filter.ProductID != 0 {
		clauses = append(clauses, "product_id = ?")
	}
	if filter.DocType != "" {
		clauses = append(clauses, "doc_type = ?")
	}
	if filter.Status != "" {
		clauses = append(clauses, "status = ?")
	}
	return `SELECT id, document_no, product_id, doc_type, version, object_key, source_hash, file_name, content_type, file_size_bytes, embedding_model, status, chunk_count, is_current_ready, ready_at, error_code, error_message, created_by_user_id, created_at, processed_at, updated_at FROM knowledge_documents WHERE ` + strings.Join(clauses, " AND ") + ` ORDER BY updated_at DESC, id DESC LIMIT ?`
}

func listDocumentArgs(filter DocumentListFilter) []any {
	args := make([]any, 0, 4)
	if filter.ProductID != 0 {
		args = append(args, filter.ProductID)
	}
	if filter.DocType != "" {
		args = append(args, filter.DocType)
	}
	if filter.Status != "" {
		args = append(args, filter.Status)
	}
	args = append(args, int(normalizeDocumentListFilter(filter).PageSize))
	return args
}

func queryKnowledgeSnippetsByChunkIDs(chunkCount int) string {
	return `SELECT c.id AS chunk_id, d.document_no, c.product_id, c.doc_type, c.version, c.section, c.source_page, c.content FROM knowledge_chunks c JOIN knowledge_documents d ON d.id = c.document_id WHERE c.product_id = ? AND c.id IN (` + placeholders(chunkCount) + `) AND c.status = 'EMBEDDED' AND d.status = 'READY' AND d.is_current_ready = 1`
}

func placeholders(count int) string {
	if count <= 0 {
		return "?"
	}
	return strings.TrimRight(strings.Repeat("?,", count), ",")
}

func scanChunk(row scanner) (Chunk, error) {
	var chunk Chunk
	var section sql.NullString
	var sourcePage sql.NullInt64
	var vectorRef sql.NullString
	if err := row.Scan(
		&chunk.ID, &chunk.DocumentID, &chunk.ProductID, &chunk.DocType, &chunk.Version, &chunk.ChunkIndex,
		&section, &sourcePage, &chunk.Content, &chunk.ContentHash, &vectorRef, &chunk.Status,
	); err != nil {
		return Chunk{}, fmt.Errorf("scan knowledge chunk: %w", err)
	}
	if section.Valid {
		chunk.Section = section.String
	}
	if sourcePage.Valid {
		page := uint32(sourcePage.Int64)
		chunk.SourcePage = &page
	}
	if vectorRef.Valid {
		chunk.VectorRef = vectorRef.String
	}
	return chunk, nil
}

func scanOpsDocument(row scanner) (Document, error) {
	var document Document
	var embeddingModel, errorCode, errorMessage sql.NullString
	var readyAt, processedAt sql.NullTime
	if err := row.Scan(
		&document.ID, &document.DocumentNo, &document.ProductID, &document.DocType, &document.Version,
		&document.ObjectKey, &document.SourceHash, &document.FileName, &document.ContentType, &document.FileSizeBytes,
		&embeddingModel, &document.Status, &document.ChunkCount, &document.IsCurrentReady, &readyAt, &errorCode, &errorMessage,
		&document.CreatedByUserID, &document.CreatedAt, &processedAt, &document.UpdatedAt,
	); err != nil {
		return Document{}, err
	}
	if embeddingModel.Valid {
		document.EmbeddingModel = embeddingModel.String
	}
	if readyAt.Valid {
		document.ReadyAt = &readyAt.Time
	}
	if errorCode.Valid {
		document.ErrorCode = errorCode.String
	}
	if errorMessage.Valid {
		document.ErrorMessage = errorMessage.String
	}
	if processedAt.Valid {
		document.ProcessedAt = &processedAt.Time
	}
	return document, nil
}

func scanKnowledgeSnippet(row scanner) (KnowledgeSnippet, error) {
	var snippet KnowledgeSnippet
	var section sql.NullString
	var sourcePage sql.NullInt64
	if err := row.Scan(&snippet.ChunkID, &snippet.DocumentNo, &snippet.ProductID, &snippet.DocType, &snippet.Version, &section, &sourcePage, &snippet.Content); err != nil {
		return KnowledgeSnippet{}, fmt.Errorf("scan knowledge snippet: %w", err)
	}
	if section.Valid {
		snippet.Section = section.String
	}
	if sourcePage.Valid {
		page := uint32(sourcePage.Int64)
		snippet.SourcePage = &page
	}
	return snippet, nil
}

func sourcePageValue(value *uint32) any {
	if value == nil {
		return nil
	}
	return *value
}

func requireSingleRow(result sql.Result, err error, operation string) error {
	if err != nil {
		return fmt.Errorf("%s: %w", operation, err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("%s rows affected: %w", operation, err)
	}
	if count != 1 {
		return fmt.Errorf("%s: row not found", operation)
	}
	return nil
}

func isDuplicate(err error) bool {
	var mysqlErr *mysql.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1062
}
