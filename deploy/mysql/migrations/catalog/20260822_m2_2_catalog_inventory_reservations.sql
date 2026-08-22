CREATE TABLE IF NOT EXISTS inventory_reservations (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    reservation_id CHAR(36) NOT NULL,
    order_no VARCHAR(64) NOT NULL,
    payment_attempt_id CHAR(36) NOT NULL,
    sku_id BIGINT UNSIGNED NOT NULL,
    quantity INT UNSIGNED NOT NULL,
    status VARCHAR(16) NOT NULL,
    expires_at DATETIME(3) NOT NULL,
    confirmed_at DATETIME(3) NULL,
    released_at DATETIME(3) NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uq_inventory_reservation_sku (reservation_id, sku_id),
    KEY idx_inventory_reservation_status_expiry (status, expires_at, id),
    CONSTRAINT chk_inventory_reservation_quantity_positive CHECK (quantity > 0),
    CONSTRAINT chk_inventory_reservation_status CHECK (status IN ('RESERVED', 'CONFIRMED', 'RELEASED')),
    CONSTRAINT fk_inventory_reservation_sku FOREIGN KEY (sku_id) REFERENCES product_skus(id)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS event_consumptions (
    event_id CHAR(36) NOT NULL,
    consumer_group VARCHAR(128) NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'CONSUMED',
    consumed_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (event_id, consumer_group)
) ENGINE=InnoDB;

SET @legacy_reservation_event_consumptions_exists := (
    SELECT COUNT(*)
    FROM information_schema.tables
    WHERE table_schema = DATABASE() AND table_name = 'reservation_event_consumptions'
);
SET @copy_reservation_event_consumptions := IF(
    @legacy_reservation_event_consumptions_exists = 1,
    'INSERT IGNORE INTO event_consumptions (event_id, consumer_group, status, consumed_at, created_at) SELECT event_id, consumer_group, ''CONSUMED'', consumed_at, consumed_at FROM reservation_event_consumptions',
    'SELECT 1'
);
PREPARE copy_reservation_event_consumptions FROM @copy_reservation_event_consumptions;
EXECUTE copy_reservation_event_consumptions;
DEALLOCATE PREPARE copy_reservation_event_consumptions;
SET @drop_reservation_event_consumptions := IF(
    @legacy_reservation_event_consumptions_exists = 1,
    'DROP TABLE reservation_event_consumptions',
    'SELECT 1'
);
PREPARE drop_reservation_event_consumptions FROM @drop_reservation_event_consumptions;
EXECUTE drop_reservation_event_consumptions;
DEALLOCATE PREPARE drop_reservation_event_consumptions;
