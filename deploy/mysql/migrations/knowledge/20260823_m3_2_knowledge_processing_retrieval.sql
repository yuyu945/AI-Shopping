ALTER TABLE knowledge_documents
  ADD COLUMN is_current_ready TINYINT(1) NOT NULL DEFAULT 0 AFTER chunk_count,
  ADD COLUMN ready_at DATETIME(3) NULL AFTER is_current_ready,
  ADD KEY idx_knowledge_document_current (product_id, doc_type, is_current_ready, ready_at);

CREATE TABLE IF NOT EXISTS knowledge_chunks (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  document_id BIGINT UNSIGNED NOT NULL,
  product_id BIGINT UNSIGNED NOT NULL,
  doc_type VARCHAR(32) NOT NULL,
  version INT UNSIGNED NOT NULL,
  chunk_index INT UNSIGNED NOT NULL,
  section VARCHAR(255) NULL,
  source_page INT UNSIGNED NULL,
  content TEXT NOT NULL,
  content_hash CHAR(64) NOT NULL,
  vector_ref VARCHAR(128) NULL,
  status VARCHAR(32) NOT NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uq_knowledge_chunk_index (document_id, chunk_index),
  UNIQUE KEY uq_knowledge_chunk_content (document_id, content_hash),
  KEY idx_knowledge_chunk_visible (product_id, doc_type, version, status, chunk_index),
  KEY idx_knowledge_chunk_document_status (document_id, status, chunk_index),
  CONSTRAINT fk_knowledge_chunk_document FOREIGN KEY (document_id) REFERENCES knowledge_documents(id),
  CONSTRAINT chk_knowledge_chunk_type CHECK (doc_type IN ('DETAIL','SPEC','FAQ','AFTER_SALE')),
  CONSTRAINT chk_knowledge_chunk_status CHECK (status IN ('PENDING_EMBEDDING','EMBEDDED','FAILED')),
  CONSTRAINT chk_knowledge_chunk_content CHECK (CHAR_LENGTH(content) > 0)
) ENGINE=InnoDB;
