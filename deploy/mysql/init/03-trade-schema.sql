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
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3), updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3), paid_at DATETIME(3) NULL, closed_at DATETIME(3) NULL,
    PRIMARY KEY (id), UNIQUE KEY uq_orders_order_no (order_no), UNIQUE KEY uq_orders_user_request (user_id, request_id), KEY idx_orders_user_created (user_id, created_at)
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
