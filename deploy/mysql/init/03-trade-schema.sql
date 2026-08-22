USE trade_db;

CREATE TABLE carts (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    user_id BIGINT UNSIGNED NOT NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id), UNIQUE KEY uq_carts_user (user_id)
) ENGINE=InnoDB;
CREATE TABLE cart_items (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    cart_id BIGINT UNSIGNED NOT NULL, sku_id BIGINT UNSIGNED NOT NULL,
    quantity INT UNSIGNED NOT NULL, selected BOOLEAN NOT NULL DEFAULT TRUE,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3), updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id), UNIQUE KEY uq_cart_items_cart_sku (cart_id, sku_id), KEY idx_cart_items_cart (cart_id),
    CONSTRAINT chk_cart_items_quantity_positive CHECK (quantity > 0),
    CONSTRAINT fk_cart_items_cart FOREIGN KEY (cart_id) REFERENCES carts(id)
) ENGINE=InnoDB;
CREATE TABLE orders (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT, order_no VARCHAR(64) NOT NULL, user_id BIGINT UNSIGNED NOT NULL, request_id VARCHAR(128) NOT NULL,
    status VARCHAR(32) NOT NULL, total_amount DECIMAL(12,2) NOT NULL, paid_amount DECIMAL(12,2) NOT NULL DEFAULT 0.00,
    shipping_name_snapshot VARCHAR(128) NOT NULL, shipping_phone_snapshot VARCHAR(32) NOT NULL, shipping_address_snapshot JSON NOT NULL,
    payment_attempt_id CHAR(36) NULL, reservation_id CHAR(36) NULL, payment_started_at DATETIME(3) NULL,
    payment_recovery_token CHAR(36) NULL, payment_recovery_lease_until DATETIME(3) NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3), updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3), paid_at DATETIME(3) NULL, closed_at DATETIME(3) NULL,
    PRIMARY KEY (id), UNIQUE KEY uq_orders_order_no (order_no), UNIQUE KEY uq_orders_user_request (user_id, request_id), KEY idx_orders_user_created (user_id, created_at),
    UNIQUE KEY uq_orders_payment_attempt (payment_attempt_id), UNIQUE KEY uq_orders_reservation (reservation_id), KEY idx_orders_status_payment_started (status, payment_started_at, id), KEY idx_orders_recovery_lease (status, payment_recovery_lease_until, id)
) ENGINE=InnoDB;
CREATE TABLE order_items (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT, order_id BIGINT UNSIGNED NOT NULL, product_id BIGINT UNSIGNED NOT NULL, sku_id BIGINT UNSIGNED NOT NULL,
    product_title_snapshot VARCHAR(256) NOT NULL, sku_code_snapshot VARCHAR(128) NOT NULL, sku_spec_snapshot JSON NOT NULL, promotion_snapshot JSON NOT NULL,
    candidate_promotions_snapshot JSON NOT NULL,
    unit_price DECIMAL(12,2) NOT NULL, discount_amount DECIMAL(12,2) NOT NULL DEFAULT 0.00, quantity INT UNSIGNED NOT NULL, item_amount DECIMAL(12,2) NOT NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3), PRIMARY KEY (id), KEY idx_order_items_order (order_id),
    CONSTRAINT chk_order_items_quantity_positive CHECK (quantity > 0),
    CONSTRAINT fk_order_items_order FOREIGN KEY (order_id) REFERENCES orders(id)
) ENGINE=InnoDB;
CREATE TABLE wallet_accounts (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    user_id BIGINT UNSIGNED NOT NULL,
    balance DECIMAL(12,2) NOT NULL DEFAULT 0.00,
    version BIGINT UNSIGNED NOT NULL DEFAULT 0,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id), UNIQUE KEY uq_wallet_accounts_user (user_id),
    CONSTRAINT chk_wallet_accounts_balance_nonnegative CHECK (balance >= 0)
) ENGINE=InnoDB;
CREATE TABLE wallet_ledger (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    user_id BIGINT UNSIGNED NOT NULL,
    biz_type VARCHAR(32) NOT NULL, biz_id VARCHAR(64) NOT NULL, direction VARCHAR(16) NOT NULL,
    amount DECIMAL(12,2) NOT NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id), UNIQUE KEY uq_wallet_ledger_business (biz_type, biz_id, direction), KEY idx_wallet_ledger_user_created (user_id, created_at),
    CONSTRAINT chk_wallet_ledger_amount_positive CHECK (amount > 0)
) ENGINE=InnoDB;
CREATE TABLE outbox_events (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    event_id CHAR(36) NOT NULL,
    aggregate_type VARCHAR(64) NOT NULL, aggregate_id VARCHAR(64) NOT NULL, event_type VARCHAR(64) NOT NULL,
    topic VARCHAR(128) NOT NULL, event_key VARCHAR(128) NOT NULL, payload JSON NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'PENDING', attempts INT UNSIGNED NOT NULL DEFAULT 0,
    next_retry_at DATETIME(3) NULL, locked_at DATETIME(3) NULL, lease_until DATETIME(3) NULL, claim_token CHAR(36) NULL, published_at DATETIME(3) NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3), updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id), UNIQUE KEY uq_outbox_event_id (event_id),
    UNIQUE KEY uq_outbox_aggregate_event (aggregate_type, aggregate_id, event_type),
    KEY idx_outbox_pending_retry (status, next_retry_at, id), KEY idx_outbox_processing_lease (status, lease_until, id)
) ENGINE=InnoDB;
