USE trade_db;

SET @candidate_promotions_column_exists = (
    SELECT COUNT(*) FROM information_schema.columns
    WHERE table_schema = 'trade_db'
      AND table_name = 'order_items'
      AND column_name = 'candidate_promotions_snapshot'
);
SET @candidate_promotions_add_sql = IF(
    @candidate_promotions_column_exists = 0,
    'ALTER TABLE order_items ADD COLUMN candidate_promotions_snapshot JSON NULL AFTER sku_spec_snapshot',
    'SELECT 1'
);
PREPARE candidate_promotions_add_statement FROM @candidate_promotions_add_sql;
EXECUTE candidate_promotions_add_statement;
DEALLOCATE PREPARE candidate_promotions_add_statement;

UPDATE order_items
SET candidate_promotions_snapshot = JSON_ARRAY()
WHERE candidate_promotions_snapshot IS NULL;

ALTER TABLE order_items
MODIFY COLUMN candidate_promotions_snapshot JSON NOT NULL;
