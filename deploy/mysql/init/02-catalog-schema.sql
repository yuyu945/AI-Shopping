USE catalog_db;

CREATE TABLE categories (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    parent_id BIGINT UNSIGNED NULL,
    name VARCHAR(128) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'ACTIVE',
    sort_no INT UNSIGNED NOT NULL DEFAULT 0,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    KEY idx_categories_parent_sort (parent_id, sort_no),
    CONSTRAINT fk_categories_parent FOREIGN KEY (parent_id) REFERENCES categories (id)
) ENGINE=InnoDB;

CREATE TABLE brands (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    name VARCHAR(128) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'ACTIVE',
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uq_brands_name (name)
) ENGINE=InnoDB;

CREATE TABLE products (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    category_id BIGINT UNSIGNED NOT NULL,
    brand_id BIGINT UNSIGNED NULL,
    title VARCHAR(256) NOT NULL,
    subtitle VARCHAR(512) NULL,
    detail_markdown MEDIUMTEXT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'DRAFT',
    version BIGINT UNSIGNED NOT NULL DEFAULT 1,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    deleted_at DATETIME(3) NULL,
    PRIMARY KEY (id),
    KEY idx_products_category_status_created (category_id, status, created_at),
    KEY idx_products_brand_status_created (brand_id, status, created_at),
    KEY idx_products_status_deleted_created (status, deleted_at, created_at, id),
    CONSTRAINT fk_products_category FOREIGN KEY (category_id) REFERENCES categories (id),
    CONSTRAINT fk_products_brand FOREIGN KEY (brand_id) REFERENCES brands (id)
) ENGINE=InnoDB;

CREATE TABLE product_skus (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    product_id BIGINT UNSIGNED NOT NULL,
    sku_code VARCHAR(128) NOT NULL,
    spec_json JSON NOT NULL,
    sale_price DECIMAL(12,2) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'DRAFT',
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uq_product_skus_code (sku_code),
    KEY idx_product_skus_product_status (product_id, status, id),
    CONSTRAINT fk_product_skus_product FOREIGN KEY (product_id) REFERENCES products (id)
) ENGINE=InnoDB;

CREATE TABLE product_images (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    product_id BIGINT UNSIGNED NOT NULL,
    object_key VARCHAR(512) NOT NULL,
    sort_no INT UNSIGNED NOT NULL DEFAULT 0,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    KEY idx_product_images_product_sort (product_id, sort_no),
    CONSTRAINT fk_product_images_product FOREIGN KEY (product_id) REFERENCES products (id)
) ENGINE=InnoDB;

CREATE TABLE inventory (
    sku_id BIGINT UNSIGNED NOT NULL,
    available_qty INT UNSIGNED NOT NULL DEFAULT 0,
    version BIGINT UNSIGNED NOT NULL DEFAULT 1,
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (sku_id),
    KEY idx_inventory_updated (updated_at, sku_id),
    CONSTRAINT fk_inventory_sku FOREIGN KEY (sku_id) REFERENCES product_skus (id)
) ENGINE=InnoDB;

CREATE TABLE promotion_rules (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    product_id BIGINT UNSIGNED NOT NULL,
    rule_type VARCHAR(64) NOT NULL,
    threshold_amount DECIMAL(12,2) NULL,
    discount_amount DECIMAL(12,2) NOT NULL,
    start_at DATETIME(3) NOT NULL,
    end_at DATETIME(3) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'DRAFT',
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    KEY idx_promotion_rules_product_status_time (product_id, status, start_at, end_at),
    CONSTRAINT fk_promotion_rules_product FOREIGN KEY (product_id) REFERENCES products (id)
) ENGINE=InnoDB;

CREATE TABLE cache_invalidation_tasks (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    cache_key VARCHAR(256) NOT NULL,
    execute_at DATETIME(3) NOT NULL,
    retry_count INT NOT NULL DEFAULT 0,
    status VARCHAR(16) NOT NULL,
    last_error VARCHAR(512) NULL,
    locked_at DATETIME(3) NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    executed_at DATETIME(3) NULL,
    PRIMARY KEY (id),
    KEY idx_cache_invalidation_tasks_status_execute (status, execute_at, id),
    KEY idx_cache_invalidation_tasks_status_locked (status, locked_at, id),
    KEY idx_cache_invalidation_tasks_key_execute (cache_key, execute_at)
) ENGINE=InnoDB;
