CREATE TABLE IF NOT EXISTS review_event_consumptions (
    event_id CHAR(36) NOT NULL,
    consumer_group VARCHAR(128) NOT NULL,
    status VARCHAR(32) NOT NULL,
    consumed_at DATETIME(3) NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (event_id, consumer_group)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS review_event_records (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    event_id CHAR(36) NOT NULL,
    review_no VARCHAR(64) NOT NULL,
    order_no VARCHAR(64) NOT NULL,
    user_id BIGINT UNSIGNED NOT NULL,
    product_id BIGINT UNSIGNED NOT NULL,
    sku_id BIGINT UNSIGNED NOT NULL,
    rating TINYINT UNSIGNED NOT NULL,
    content VARCHAR(1000) NOT NULL,
    occurred_at DATETIME(3) NOT NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uq_review_event_records_event (event_id),
    UNIQUE KEY uq_review_event_records_review (review_no),
    KEY idx_review_event_records_product_created (product_id, occurred_at)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS product_review_stats (
    product_id BIGINT UNSIGNED NOT NULL,
    review_count BIGINT UNSIGNED NOT NULL DEFAULT 0,
    rating_sum BIGINT UNSIGNED NOT NULL DEFAULT 0,
    rating_avg DECIMAL(4,2) NOT NULL DEFAULT 0.00,
    last_review_at DATETIME(3) NULL,
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (product_id)
) ENGINE=InnoDB;
