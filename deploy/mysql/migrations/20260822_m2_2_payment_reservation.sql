SET @payment_attempt_id_column_exists = (
    SELECT COUNT(*) FROM information_schema.columns
    WHERE table_schema = 'trade_db' AND table_name = 'orders' AND column_name = 'payment_attempt_id'
);
SET @payment_attempt_id_add_sql = IF(
    @payment_attempt_id_column_exists = 0,
    'ALTER TABLE orders ADD COLUMN payment_attempt_id CHAR(36) NULL AFTER status',
    'SELECT 1'
);
PREPARE payment_attempt_id_add_statement FROM @payment_attempt_id_add_sql;
EXECUTE payment_attempt_id_add_statement;
DEALLOCATE PREPARE payment_attempt_id_add_statement;

SET @reservation_id_column_exists = (
    SELECT COUNT(*) FROM information_schema.columns
    WHERE table_schema = 'trade_db' AND table_name = 'orders' AND column_name = 'reservation_id'
);
SET @reservation_id_add_sql = IF(
    @reservation_id_column_exists = 0,
    'ALTER TABLE orders ADD COLUMN reservation_id CHAR(36) NULL AFTER payment_attempt_id',
    'SELECT 1'
);
PREPARE reservation_id_add_statement FROM @reservation_id_add_sql;
EXECUTE reservation_id_add_statement;
DEALLOCATE PREPARE reservation_id_add_statement;

SET @payment_started_at_column_exists = (
    SELECT COUNT(*) FROM information_schema.columns
    WHERE table_schema = 'trade_db' AND table_name = 'orders' AND column_name = 'payment_started_at'
);
SET @payment_started_at_add_sql = IF(
    @payment_started_at_column_exists = 0,
    'ALTER TABLE orders ADD COLUMN payment_started_at DATETIME(3) NULL AFTER reservation_id',
    'SELECT 1'
);
PREPARE payment_started_at_add_statement FROM @payment_started_at_add_sql;
EXECUTE payment_started_at_add_statement;
DEALLOCATE PREPARE payment_started_at_add_statement;

SET @payment_recovery_token_exists = (SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = 'trade_db' AND table_name = 'orders' AND column_name = 'payment_recovery_token');
SET @payment_recovery_token_sql = IF(@payment_recovery_token_exists = 0, 'ALTER TABLE orders ADD COLUMN payment_recovery_token CHAR(36) NULL AFTER payment_started_at', 'SELECT 1');
PREPARE payment_recovery_token_statement FROM @payment_recovery_token_sql;
EXECUTE payment_recovery_token_statement;
DEALLOCATE PREPARE payment_recovery_token_statement;
SET @payment_recovery_lease_exists = (SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = 'trade_db' AND table_name = 'orders' AND column_name = 'payment_recovery_lease_until');
SET @payment_recovery_lease_sql = IF(@payment_recovery_lease_exists = 0, 'ALTER TABLE orders ADD COLUMN payment_recovery_lease_until DATETIME(3) NULL AFTER payment_recovery_token', 'SELECT 1');
PREPARE payment_recovery_lease_statement FROM @payment_recovery_lease_sql;
EXECUTE payment_recovery_lease_statement;
DEALLOCATE PREPARE payment_recovery_lease_statement;
SET @payment_recovery_lease_index_exists = (SELECT COUNT(*) FROM information_schema.statistics WHERE table_schema = 'trade_db' AND table_name = 'orders' AND index_name = 'idx_orders_recovery_lease');
SET @payment_recovery_lease_index_sql = IF(@payment_recovery_lease_index_exists = 0, 'ALTER TABLE orders ADD KEY idx_orders_recovery_lease (status, payment_recovery_lease_until, id)', 'SELECT 1');
PREPARE payment_recovery_lease_index_statement FROM @payment_recovery_lease_index_sql;
EXECUTE payment_recovery_lease_index_statement;
DEALLOCATE PREPARE payment_recovery_lease_index_statement;

SET @payment_attempt_index_exists = (
    SELECT COUNT(*) FROM information_schema.statistics
    WHERE table_schema = 'trade_db' AND table_name = 'orders' AND index_name = 'uq_orders_payment_attempt'
);
SET @payment_attempt_index_add_sql = IF(
    @payment_attempt_index_exists = 0,
    'ALTER TABLE orders ADD UNIQUE KEY uq_orders_payment_attempt (payment_attempt_id)',
    'SELECT 1'
);
PREPARE payment_attempt_index_add_statement FROM @payment_attempt_index_add_sql;
EXECUTE payment_attempt_index_add_statement;
DEALLOCATE PREPARE payment_attempt_index_add_statement;

SET @reservation_index_exists = (
    SELECT COUNT(*) FROM information_schema.statistics
    WHERE table_schema = 'trade_db' AND table_name = 'orders' AND index_name = 'uq_orders_reservation'
);
SET @reservation_index_add_sql = IF(
    @reservation_index_exists = 0,
    'ALTER TABLE orders ADD UNIQUE KEY uq_orders_reservation (reservation_id)',
    'SELECT 1'
);
PREPARE reservation_index_add_statement FROM @reservation_index_add_sql;
EXECUTE reservation_index_add_statement;
DEALLOCATE PREPARE reservation_index_add_statement;

SET @payment_recovery_index_exists = (
    SELECT COUNT(*) FROM information_schema.statistics
    WHERE table_schema = 'trade_db' AND table_name = 'orders' AND index_name = 'idx_orders_status_payment_started'
);
SET @payment_recovery_index_add_sql = IF(
    @payment_recovery_index_exists = 0,
    'ALTER TABLE orders ADD KEY idx_orders_status_payment_started (status, payment_started_at, id)',
    'SELECT 1'
);
PREPARE payment_recovery_index_add_statement FROM @payment_recovery_index_add_sql;
EXECUTE payment_recovery_index_add_statement;
DEALLOCATE PREPARE payment_recovery_index_add_statement;

CREATE TABLE IF NOT EXISTS payment_attempt_history (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    order_no VARCHAR(64) NOT NULL,
    payment_attempt_id CHAR(36) NOT NULL,
    reservation_id CHAR(36) NOT NULL,
    status VARCHAR(32) NOT NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id), UNIQUE KEY uq_payment_attempt_history_attempt (payment_attempt_id),
    UNIQUE KEY uq_payment_attempt_history_reservation (reservation_id), KEY idx_payment_attempt_history_order (order_no, payment_attempt_id)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS wallet_accounts (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    user_id BIGINT UNSIGNED NOT NULL,
    balance DECIMAL(12,2) NOT NULL DEFAULT 0.00,
    version BIGINT UNSIGNED NOT NULL DEFAULT 0,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id), UNIQUE KEY uq_wallet_accounts_user (user_id),
    CONSTRAINT chk_wallet_accounts_balance_nonnegative CHECK (balance >= 0)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS wallet_ledger (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    user_id BIGINT UNSIGNED NOT NULL,
    biz_type VARCHAR(32) NOT NULL, biz_id VARCHAR(64) NOT NULL, direction VARCHAR(16) NOT NULL,
    amount DECIMAL(12,2) NOT NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id), UNIQUE KEY uq_wallet_ledger_business (biz_type, biz_id, direction), KEY idx_wallet_ledger_user_created (user_id, created_at),
    CONSTRAINT chk_wallet_ledger_amount_positive CHECK (amount > 0)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS outbox_events (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    event_id CHAR(36) NOT NULL,
    aggregate_type VARCHAR(64) NOT NULL, aggregate_id VARCHAR(64) NOT NULL, event_type VARCHAR(64) NOT NULL,
    topic VARCHAR(128) NOT NULL, event_key VARCHAR(128) NOT NULL, payload JSON NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'PENDING', attempts INT UNSIGNED NOT NULL DEFAULT 0,
    next_retry_at DATETIME(3) NULL, published_at DATETIME(3) NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3), updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id), UNIQUE KEY uq_outbox_event_id (event_id),
    UNIQUE KEY uq_outbox_aggregate_event (aggregate_type, aggregate_id, event_type),
    KEY idx_outbox_pending_retry (status, next_retry_at, id)
) ENGINE=InnoDB;

SET @outbox_locked_at_exists = (SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = 'trade_db' AND table_name = 'outbox_events' AND column_name = 'locked_at');
SET @outbox_locked_at_sql = IF(@outbox_locked_at_exists = 0, 'ALTER TABLE outbox_events ADD COLUMN locked_at DATETIME(3) NULL AFTER next_retry_at', 'SELECT 1');
PREPARE outbox_locked_at_statement FROM @outbox_locked_at_sql;
EXECUTE outbox_locked_at_statement;
DEALLOCATE PREPARE outbox_locked_at_statement;

SET @outbox_lease_until_exists = (SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = 'trade_db' AND table_name = 'outbox_events' AND column_name = 'lease_until');
SET @outbox_lease_until_sql = IF(@outbox_lease_until_exists = 0, 'ALTER TABLE outbox_events ADD COLUMN lease_until DATETIME(3) NULL AFTER locked_at', 'SELECT 1');
PREPARE outbox_lease_until_statement FROM @outbox_lease_until_sql;
EXECUTE outbox_lease_until_statement;
DEALLOCATE PREPARE outbox_lease_until_statement;

SET @outbox_claim_token_exists = (SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = 'trade_db' AND table_name = 'outbox_events' AND column_name = 'claim_token');
SET @outbox_claim_token_sql = IF(@outbox_claim_token_exists = 0, 'ALTER TABLE outbox_events ADD COLUMN claim_token CHAR(36) NULL AFTER lease_until', 'SELECT 1');
PREPARE outbox_claim_token_statement FROM @outbox_claim_token_sql;
EXECUTE outbox_claim_token_statement;
DEALLOCATE PREPARE outbox_claim_token_statement;

SET @outbox_processing_lease_index_exists = (SELECT COUNT(*) FROM information_schema.statistics WHERE table_schema = 'trade_db' AND table_name = 'outbox_events' AND index_name = 'idx_outbox_processing_lease');
SET @outbox_processing_lease_index_sql = IF(@outbox_processing_lease_index_exists = 0, 'ALTER TABLE outbox_events ADD KEY idx_outbox_processing_lease (status, lease_until, id)', 'SELECT 1');
PREPARE outbox_processing_lease_index_statement FROM @outbox_processing_lease_index_sql;
EXECUTE outbox_processing_lease_index_statement;
DEALLOCATE PREPARE outbox_processing_lease_index_statement;
