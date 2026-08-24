USE agent_db;

CREATE TABLE IF NOT EXISTS recommendations (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    run_id BIGINT UNSIGNED NOT NULL,
    rank_no INT UNSIGNED NOT NULL,
    sku_id BIGINT UNSIGNED NOT NULL,
    product_id BIGINT UNSIGNED NOT NULL,
    product_title_snapshot VARCHAR(255) NOT NULL,
    sku_code_snapshot VARCHAR(128) NOT NULL,
    sku_spec_snapshot_json JSON NOT NULL,
    price_snapshot DECIMAL(12,2) NOT NULL,
    saleable_snapshot TINYINT(1) NOT NULL,
    discount_snapshot_json JSON NOT NULL,
    reason VARCHAR(512) NOT NULL,
    validation_status VARCHAR(32) NOT NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uq_recommendation_rank (run_id, rank_no),
    UNIQUE KEY uq_recommendation_sku (run_id, sku_id),
    KEY idx_recommendation_run_rank (run_id, rank_no),
    CONSTRAINT chk_recommendation_status CHECK (validation_status IN ('VERIFIED')),
    CONSTRAINT fk_recommendation_run FOREIGN KEY (run_id) REFERENCES agent_runs(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
