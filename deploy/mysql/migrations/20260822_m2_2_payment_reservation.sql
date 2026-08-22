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
