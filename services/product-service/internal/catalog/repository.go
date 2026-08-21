package catalog

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	_ "github.com/go-sql-driver/mysql"
)

// NotFoundError indicates that an active, non-deleted catalog resource was not found.
type NotFoundError struct {
	Resource string
	ID       uint64
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("%s %d not found", e.Resource, e.ID)
}

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// Repository reads catalog data from MySQL. It deliberately exposes values, not sql.Rows.
type Repository struct {
	db queryer
}

// NewRepository constructs a catalog repository backed by database/sql.
func NewRepository(db queryer) *Repository {
	return &Repository{db: db}
}

// ListProducts returns active, non-deleted products in deterministic pages.
func (r *Repository) ListProducts(ctx context.Context, filter ProductFilter) ([]ProductSummary, error) {
	page, pageSize := normalizePage(filter.Page, filter.PageSize)
	offset := (page - 1) * pageSize
	conditions := []string{"p.status = 'ACTIVE'", "p.deleted_at IS NULL"}
	args := make([]any, 0, 5)
	keyword := strings.TrimSpace(filter.Keyword)
	if keyword != "" {
		conditions = append(conditions, "(p.title LIKE ? OR p.subtitle LIKE ?)")
		pattern := "%" + keyword + "%"
		args = append(args, pattern, pattern)
	}
	if filter.CategoryID != 0 {
		conditions = append(conditions, "p.category_id = ?")
		args = append(args, filter.CategoryID)
	}
	args = append(args, pageSize, offset)
	query := "SELECT p.id, p.category_id, p.brand_id, p.title, p.subtitle, MIN(ps.sale_price), COALESCE(SUM(i.available_qty), 0) " +
		"FROM products p LEFT JOIN product_skus ps ON ps.product_id = p.id AND ps.status = 'ACTIVE' " +
		"LEFT JOIN inventory i ON i.sku_id = ps.id WHERE " + strings.Join(conditions, " AND ") +
		" GROUP BY p.id, p.category_id, p.brand_id, p.title, p.subtitle ORDER BY p.created_at DESC, p.id DESC LIMIT ? OFFSET ?"
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list products: %w", err)
	}
	defer rows.Close()
	products := make([]ProductSummary, 0)
	for rows.Next() {
		var p ProductSummary
		var subtitle, minPrice sql.NullString
		if err := rows.Scan(&p.ID, &p.CategoryID, &p.BrandID, &p.Title, &subtitle, &minPrice, &p.StockQty); err != nil {
			return nil, fmt.Errorf("scan product summary: %w", err)
		}
		if subtitle.Valid {
			p.Subtitle = &subtitle.String
		}
		if minPrice.Valid {
			p.MinPrice = &minPrice.String
		}
		products = append(products, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate products: %w", err)
	}
	return products, nil
}

// GetProduct returns one active product and optionally restricts the returned SKU.
func (r *Repository) GetProduct(ctx context.Context, productID uint64, optionalSKUID *uint64) (ProductDetail, error) {
	var detail ProductDetail
	row := r.db.QueryRowContext(ctx, "SELECT p.id, p.category_id, p.brand_id, p.title, p.subtitle, p.detail_markdown FROM products p WHERE p.id = ? AND p.status = 'ACTIVE' AND p.deleted_at IS NULL", productID)
	var subtitle, detailMarkdown sql.NullString
	if err := row.Scan(&detail.ID, &detail.CategoryID, &detail.BrandID, &detail.Title, &subtitle, &detailMarkdown); err != nil {
		if err == sql.ErrNoRows {
			return ProductDetail{}, &NotFoundError{Resource: "product", ID: productID}
		}
		return ProductDetail{}, fmt.Errorf("get product: %w", err)
	}
	if subtitle.Valid {
		detail.Subtitle = &subtitle.String
	}
	if detailMarkdown.Valid {
		detail.DetailMarkdown = &detailMarkdown.String
	}
	skuQuery := "SELECT ps.id, ps.sku_code, ps.spec_json, ps.sale_price, i.available_qty, i.version FROM product_skus ps LEFT JOIN inventory i ON i.sku_id = ps.id WHERE ps.product_id = ? AND ps.status = 'ACTIVE'"
	skuArgs := []any{productID}
	if optionalSKUID != nil {
		skuQuery += " AND ps.id = ?"
		skuArgs = append(skuArgs, *optionalSKUID)
	}
	skuQuery += " ORDER BY ps.id ASC"
	rows, err := r.db.QueryContext(ctx, skuQuery, skuArgs...)
	if err != nil {
		return ProductDetail{}, fmt.Errorf("get product skus: %w", err)
	}
	for rows.Next() {
		var sku SKUDetail
		var spec string
		var availableQty, inventoryVersion sql.NullInt64
		if err := rows.Scan(&sku.ID, &sku.SKUCode, &spec, &sku.SalePrice, &availableQty, &inventoryVersion); err != nil {
			rows.Close()
			return ProductDetail{}, fmt.Errorf("scan product sku: %w", err)
		}
		sku.SpecJSON = []byte(spec)
		if availableQty.Valid && availableQty.Int64 > 0 {
			sku.Inventory.AvailableQty = uint64(availableQty.Int64)
		}
		if inventoryVersion.Valid && inventoryVersion.Int64 > 0 {
			sku.Inventory.Version = uint64(inventoryVersion.Int64)
		}
		detail.SKUs = append(detail.SKUs, sku)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return ProductDetail{}, fmt.Errorf("iterate product skus: %w", err)
	}
	if err := rows.Close(); err != nil {
		return ProductDetail{}, fmt.Errorf("close product skus: %w", err)
	}
	if optionalSKUID != nil && len(detail.SKUs) == 0 {
		return ProductDetail{}, &NotFoundError{Resource: "sku", ID: *optionalSKUID}
	}
	imageRows, err := r.db.QueryContext(ctx, "SELECT id, object_key, sort_no FROM product_images WHERE product_id = ? ORDER BY sort_no ASC, id ASC", productID)
	if err != nil {
		return ProductDetail{}, fmt.Errorf("get product images: %w", err)
	}
	defer imageRows.Close()
	for imageRows.Next() {
		var image ImageRef
		if err := imageRows.Scan(&image.ID, &image.ObjectKey, &image.SortNo); err != nil {
			return ProductDetail{}, fmt.Errorf("scan product image: %w", err)
		}
		detail.Images = append(detail.Images, image)
	}
	if err := imageRows.Err(); err != nil {
		return ProductDetail{}, fmt.Errorf("iterate product images: %w", err)
	}
	promotionRows, err := r.db.QueryContext(ctx, "SELECT id, rule_type, threshold_amount, discount_amount FROM promotion_rules WHERE product_id = ? AND status = 'ACTIVE' AND start_at <= NOW() AND end_at > NOW() ORDER BY id ASC", productID)
	if err != nil {
		return ProductDetail{}, fmt.Errorf("get product promotions: %w", err)
	}
	defer promotionRows.Close()
	for promotionRows.Next() {
		var promotion PromotionSummary
		var threshold, discount sql.NullString
		if err := promotionRows.Scan(&promotion.ID, &promotion.RuleType, &threshold, &discount); err != nil {
			return ProductDetail{}, fmt.Errorf("scan product promotion: %w", err)
		}
		if threshold.Valid {
			promotion.ThresholdAmount = &threshold.String
		}
		if discount.Valid {
			promotion.DiscountAmount = &discount.String
		}
		detail.Promotions = append(detail.Promotions, promotion)
	}
	if err := promotionRows.Err(); err != nil {
		return ProductDetail{}, fmt.Errorf("iterate product promotions: %w", err)
	}
	return detail, nil
}

func normalizePage(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}
