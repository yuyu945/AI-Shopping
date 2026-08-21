USE catalog_db;

INSERT INTO categories (id, parent_id, name, status, sort_no)
VALUES
    (1001, NULL, 'Laptops', 'ACTIVE', 10),
    (1002, NULL, 'Phones', 'ACTIVE', 20)
ON DUPLICATE KEY UPDATE name = VALUES(name), status = VALUES(status), sort_no = VALUES(sort_no);

INSERT INTO brands (id, name, status)
VALUES
    (1001, 'Northstar', 'ACTIVE'),
    (1002, 'Clearwave', 'ACTIVE')
ON DUPLICATE KEY UPDATE name = VALUES(name), status = VALUES(status);

INSERT INTO products (id, category_id, brand_id, title, subtitle, detail_markdown, status, version, deleted_at)
VALUES
    (1001, 1001, 1001, 'Northstar Air 14', 'Portable work laptop', '14-inch productivity laptop.', 'ACTIVE', 1, NULL),
    (1002, 1001, 1001, 'Northstar Pro 16', 'Performance laptop', '16-inch creator laptop.', 'ACTIVE', 1, NULL),
    (1003, 1002, 1002, 'Clearwave X1', 'Everyday 5G phone', 'Balanced phone for daily use.', 'ACTIVE', 1, NULL),
    (1004, 1002, 1002, 'Clearwave Legacy', 'Archived phone model', 'Inactive catalog item.', 'INACTIVE', 1, NULL)
ON DUPLICATE KEY UPDATE
    category_id = VALUES(category_id), brand_id = VALUES(brand_id), title = VALUES(title),
    subtitle = VALUES(subtitle), detail_markdown = VALUES(detail_markdown), status = VALUES(status),
    version = VALUES(version), deleted_at = VALUES(deleted_at);

INSERT INTO product_skus (id, product_id, sku_code, spec_json, sale_price, status)
VALUES
    (1101, 1001, 'AIR14-I5-16', '{"cpu":"i5","memory":"16GB","storage":"512GB"}', 5999.00, 'ACTIVE'),
    (1102, 1001, 'AIR14-I7-32', '{"cpu":"i7","memory":"32GB","storage":"1TB"}', 7499.00, 'ACTIVE'),
    (1201, 1002, 'PRO16-I7-32', '{"cpu":"i7","memory":"32GB","storage":"1TB"}', 8999.00, 'ACTIVE'),
    (1202, 1002, 'PRO16-I9-64', '{"cpu":"i9","memory":"64GB","storage":"2TB"}', 11999.00, 'ACTIVE'),
    (1301, 1003, 'X1-256-BLACK', '{"storage":"256GB","color":"black"}', 3299.00, 'ACTIVE'),
    (1302, 1003, 'X1-512-WHITE', '{"storage":"512GB","color":"white"}', 3899.00, 'ACTIVE'),
    (1401, 1004, 'LEGACY-128', '{"storage":"128GB","color":"gray"}', 1299.00, 'INACTIVE')
ON DUPLICATE KEY UPDATE
    product_id = VALUES(product_id), spec_json = VALUES(spec_json), sale_price = VALUES(sale_price), status = VALUES(status);

INSERT INTO inventory (sku_id, available_qty, version)
VALUES
    (1101, 24, 1), (1102, 8, 1), (1201, 12, 1), (1202, 4, 1),
    (1301, 30, 1), (1302, 15, 1), (1401, 0, 1)
ON DUPLICATE KEY UPDATE available_qty = VALUES(available_qty), version = VALUES(version);

INSERT INTO product_images (id, product_id, object_key, sort_no)
VALUES
    (10001, 1001, 'catalog/northstar-air-14.jpg', 0),
    (10002, 1001, 'catalog/northstar-air-14-side.jpg', 1),
    (10003, 1002, 'catalog/northstar-pro-16.jpg', 0),
    (10004, 1003, 'catalog/clearwave-x1.jpg', 0),
    (10005, 1004, 'catalog/clearwave-legacy.jpg', 0)
ON DUPLICATE KEY UPDATE product_id = VALUES(product_id), object_key = VALUES(object_key), sort_no = VALUES(sort_no);

INSERT INTO promotion_rules (id, product_id, rule_type, threshold_amount, discount_amount, start_at, end_at, status)
VALUES
    (20001, 1001, 'DIRECT_DISCOUNT', NULL, 300.00, '2026-01-01 00:00:00.000', '2099-12-31 23:59:59.999', 'ACTIVE')
ON DUPLICATE KEY UPDATE
    product_id = VALUES(product_id), rule_type = VALUES(rule_type), threshold_amount = VALUES(threshold_amount),
    discount_amount = VALUES(discount_amount), start_at = VALUES(start_at), end_at = VALUES(end_at), status = VALUES(status);
