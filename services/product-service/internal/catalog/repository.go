package catalog

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"sort"
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

var decimal12_2Pattern = regexp.MustCompile(`^[0-9]{1,10}\.[0-9]{2}$`)

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

// CheckoutSKUs reads current checkout facts directly from MySQL. It does not
// use cache-aware ProductService.GetProduct and does not claim inventory.
func (r *Repository) CheckoutSKUs(ctx context.Context, skuIDs []uint64) ([]CheckoutSKU, error) {
	if len(skuIDs) == 0 {
		return nil, nil
	}
	queryIDs := uniqueSKUIds(skuIDs)
	placeholders := strings.TrimRight(strings.Repeat("?,", len(queryIDs)), ",")
	args := make([]any, len(queryIDs))
	for i, skuID := range queryIDs {
		args[i] = skuID
	}
	query := "SELECT ps.id, ps.product_id, p.title, ps.sku_code, ps.spec_json, ps.sale_price, " +
		"CASE WHEN p.status = 'ACTIVE' AND p.deleted_at IS NULL AND ps.status = 'ACTIVE' THEN 1 ELSE 0 END " +
		"FROM product_skus ps JOIN products p ON p.id = ps.product_id WHERE ps.id IN (" + placeholders + ")"
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("checkout skus: %w", err)
	}
	defer rows.Close()
	bySKU := make(map[uint64]CheckoutSKU, len(queryIDs))
	for rows.Next() {
		var row CheckoutSKU
		var spec string
		if err := rows.Scan(&row.SKUID, &row.ProductID, &row.ProductTitle, &row.SKUCode, &spec, &row.SalePrice, &row.Saleable); err != nil {
			return nil, fmt.Errorf("scan checkout sku: %w", err)
		}
		row.SpecJSON = []byte(spec)
		if !decimal12_2Pattern.MatchString(row.SalePrice) {
			return nil, fmt.Errorf("invalid checkout sku price")
		}
		bySKU[row.SKUID] = row
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate checkout skus: %w", err)
	}
	productIDs := make([]uint64, 0, len(queryIDs))
	seenProducts := make(map[uint64]struct{}, len(queryIDs))
	for _, skuID := range skuIDs {
		row, ok := bySKU[skuID]
		if !ok {
			return nil, &NotFoundError{Resource: "sku", ID: skuID}
		}
		if _, seen := seenProducts[row.ProductID]; !seen {
			seenProducts[row.ProductID] = struct{}{}
			productIDs = append(productIDs, row.ProductID)
		}
	}
	sort.Slice(productIDs, func(i, j int) bool { return productIDs[i] < productIDs[j] })
	promotions, err := r.checkoutPromotions(ctx, productIDs)
	if err != nil {
		return nil, err
	}
	result := make([]CheckoutSKU, 0, len(skuIDs))
	for _, skuID := range skuIDs {
		row := bySKU[skuID]
		row.Promotions = clonePromotions(promotions[row.ProductID])
		result = append(result, row)
	}
	return result, nil
}

func (r *Repository) checkoutPromotions(ctx context.Context, productIDs []uint64) (map[uint64][]PromotionSummary, error) {
	if len(productIDs) == 0 {
		return map[uint64][]PromotionSummary{}, nil
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(productIDs)), ",")
	args := make([]any, len(productIDs))
	for i, productID := range productIDs {
		args[i] = productID
	}
	query := "SELECT product_id, id, rule_type, threshold_amount, discount_amount FROM promotion_rules WHERE product_id IN (" + placeholders + ") AND status = 'ACTIVE' AND start_at <= NOW(3) AND end_at > NOW(3) ORDER BY product_id ASC, id ASC"
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("checkout promotions: %w", err)
	}
	defer rows.Close()
	result := make(map[uint64][]PromotionSummary, len(productIDs))
	for rows.Next() {
		var productID uint64
		var row PromotionSummary
		var threshold, discount sql.NullString
		if err := rows.Scan(&productID, &row.ID, &row.RuleType, &threshold, &discount); err != nil {
			return nil, fmt.Errorf("scan checkout promotion: %w", err)
		}
		if threshold.Valid {
			row.ThresholdAmount = &threshold.String
		}
		if discount.Valid {
			row.DiscountAmount = &discount.String
		}
		result[productID] = append(result[productID], row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate checkout promotions: %w", err)
	}
	return result, nil
}

func uniqueSKUIds(skuIDs []uint64) []uint64 {
	seen := make(map[uint64]struct{}, len(skuIDs))
	result := make([]uint64, 0, len(skuIDs))
	for _, skuID := range skuIDs {
		if _, ok := seen[skuID]; ok {
			continue
		}
		seen[skuID] = struct{}{}
		result = append(result, skuID)
	}
	return result
}

func clonePromotions(promotions []PromotionSummary) []PromotionSummary {
	result := append([]PromotionSummary(nil), promotions...)
	for i := range result {
		result[i].ThresholdAmount = cloneStringPtr(promotions[i].ThresholdAmount)
		result[i].DiscountAmount = cloneStringPtr(promotions[i].DiscountAmount)
	}
	return result
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
