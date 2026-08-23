USE knowledge_db;

CREATE TABLE IF NOT EXISTS schema_migrations (
  version VARCHAR(128) NOT NULL,
  applied_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (version)
) ENGINE=InnoDB;

CREATE TABLE knowledge_documents (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  document_no VARCHAR(64) NOT NULL,
  product_id BIGINT UNSIGNED NOT NULL,
  doc_type VARCHAR(32) NOT NULL,
  version INT UNSIGNED NOT NULL,
  object_key VARCHAR(512) NOT NULL,
  source_hash CHAR(64) NOT NULL,
  file_name VARCHAR(255) NOT NULL,
  content_type VARCHAR(128) NOT NULL,
  file_size_bytes BIGINT UNSIGNED NOT NULL,
  embedding_model VARCHAR(128) NULL,
  status VARCHAR(32) NOT NULL,
  chunk_count INT UNSIGNED NOT NULL DEFAULT 0,
  is_current_ready TINYINT(1) NOT NULL DEFAULT 0,
  ready_at DATETIME(3) NULL,
  error_code VARCHAR(64) NULL,
  error_message VARCHAR(255) NULL,
  created_by_user_id BIGINT UNSIGNED NOT NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  processed_at DATETIME(3) NULL,
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uq_knowledge_document_no (document_no),
  UNIQUE KEY uq_knowledge_document_version (product_id, doc_type, version),
  UNIQUE KEY uq_knowledge_document_source (product_id, doc_type, source_hash),
  KEY idx_knowledge_document_product_status (product_id, doc_type, status, processed_at),
  KEY idx_knowledge_document_current (product_id, doc_type, is_current_ready, ready_at),
  CONSTRAINT chk_knowledge_document_type CHECK (doc_type IN ('DETAIL','SPEC','FAQ','AFTER_SALE')),
  CONSTRAINT chk_knowledge_document_status CHECK (status IN ('PENDING','PROCESSING','READY','FAILED')),
  CONSTRAINT chk_knowledge_document_file_size CHECK (file_size_bytes > 0)
) ENGINE=InnoDB;

CREATE TABLE knowledge_chunks (
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

CREATE TABLE outbox_events (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  event_id CHAR(36) NOT NULL,
  aggregate_type VARCHAR(64) NOT NULL,
  aggregate_id VARCHAR(64) NOT NULL,
  event_type VARCHAR(128) NOT NULL,
  topic VARCHAR(128) NOT NULL,
  event_key VARCHAR(128) NOT NULL,
  payload JSON NOT NULL,
  status VARCHAR(16) NOT NULL DEFAULT 'PENDING',
  attempts INT UNSIGNED NOT NULL DEFAULT 0,
  next_retry_at DATETIME(3) NULL,
  locked_at DATETIME(3) NULL,
  lease_until DATETIME(3) NULL,
  claim_token CHAR(36) NULL,
  last_error VARCHAR(255) NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  published_at DATETIME(3) NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uq_knowledge_outbox_event (event_id),
  KEY idx_knowledge_outbox_status_retry (status, next_retry_at, id),
  KEY idx_knowledge_outbox_topic_key (topic, event_key),
  CONSTRAINT chk_knowledge_outbox_status CHECK (status IN ('PENDING','PROCESSING','PUBLISHED','DEAD'))
) ENGINE=InnoDB;

CREATE TABLE event_consumptions (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  event_id CHAR(36) NOT NULL,
  consumer_group VARCHAR(128) NOT NULL,
  status VARCHAR(16) NOT NULL,
  consumed_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uq_knowledge_event_consumption (event_id, consumer_group),
  CONSTRAINT chk_knowledge_event_consumption_status CHECK (status IN ('SUCCEEDED','FAILED'))
) ENGINE=InnoDB;

CREATE TABLE embedding_tasks (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  event_id CHAR(36) NOT NULL,
  document_id BIGINT UNSIGNED NOT NULL,
  version INT UNSIGNED NOT NULL,
  status VARCHAR(16) NOT NULL,
  retry_count INT UNSIGNED NOT NULL DEFAULT 0,
  next_retry_at DATETIME(3) NULL,
  last_error VARCHAR(255) NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uq_knowledge_embedding_event (event_id),
  KEY idx_knowledge_embedding_status_retry (status, next_retry_at, id),
  KEY idx_knowledge_embedding_document (document_id, version),
  CONSTRAINT fk_knowledge_embedding_document FOREIGN KEY (document_id) REFERENCES knowledge_documents(id),
  CONSTRAINT chk_knowledge_embedding_status CHECK (status IN ('PENDING','PROCESSING','DONE','FAILED','DEAD'))
) ENGINE=InnoDB;
