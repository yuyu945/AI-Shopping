package catalog

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestRepositoryListProductsFiltersAndPaginates(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	query := regexp.QuoteMeta("SELECT p.id, p.category_id, p.brand_id, p.title, p.subtitle, MIN(ps.sale_price), COALESCE(SUM(i.available_qty), 0) FROM products p LEFT JOIN product_skus ps ON ps.product_id = p.id AND ps.status = 'ACTIVE' LEFT JOIN inventory i ON i.sku_id = ps.id WHERE p.status = 'ACTIVE' AND p.deleted_at IS NULL AND (p.title LIKE ? OR p.subtitle LIKE ?) AND p.category_id = ? GROUP BY p.id, p.category_id, p.brand_id, p.title, p.subtitle ORDER BY p.created_at DESC, p.id DESC LIMIT ? OFFSET ?")
	mock.ExpectQuery(query).
		WithArgs("%phone%", "%phone%", uint64(9), 20, 20).
		WillReturnRows(sqlmock.NewRows([]string{"id", "category_id", "brand_id", "title", "subtitle", "min_price", "stock_qty"}).
			AddRow(uint64(10), uint64(9), uint64(3), "Phone", "Fast", "99.90", uint64(5)))

	repo := NewRepository(db)
	got, err := repo.ListProducts(context.Background(), ProductFilter{Keyword: "phone", CategoryID: 9, Page: 2, PageSize: 20})
	if err != nil {
		t.Fatalf("ListProducts() error = %v", err)
	}
	if len(got) != 1 || got[0].ID != 10 || got[0].StockQty != 5 {
		t.Fatalf("unexpected products: %#v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRepositoryListProductsAllowsActiveProductWithoutActiveSKU(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT p.id, p.category_id, p.brand_id, p.title, p.subtitle, MIN(ps.sale_price), COALESCE(SUM(i.available_qty), 0) FROM products p LEFT JOIN product_skus ps ON ps.product_id = p.id AND ps.status = 'ACTIVE' LEFT JOIN inventory i ON i.sku_id = ps.id WHERE p.status = 'ACTIVE' AND p.deleted_at IS NULL GROUP BY p.id, p.category_id, p.brand_id, p.title, p.subtitle ORDER BY p.created_at DESC, p.id DESC LIMIT ? OFFSET ?")).
		WithArgs(100, 0).
		WillReturnRows(sqlmock.NewRows([]string{"id", "category_id", "brand_id", "title", "subtitle", "min_price", "stock_qty"}).
			AddRow(uint64(11), uint64(9), nil, "Unconfigured", nil, nil, uint64(0)))

	got, err := NewRepository(db).ListProducts(context.Background(), ProductFilter{Page: 1, PageSize: 1000})
	if err != nil {
		t.Fatalf("ListProducts() error = %v", err)
	}
	if len(got) != 1 || got[0].StockQty != 0 || got[0].MinPrice != nil || got[0].Subtitle != nil {
		t.Fatalf("unexpected zero-SKU product: %#v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRepositoryGetProductLoadsActiveSKUsInventoryAndImages(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectQuery(regexp.QuoteMeta("SELECT p.id, p.category_id, p.brand_id, p.title, p.subtitle, p.detail_markdown FROM products p WHERE p.id = ? AND p.status = 'ACTIVE' AND p.deleted_at IS NULL")).
		WithArgs(uint64(10)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "category_id", "brand_id", "title", "subtitle", "detail_markdown"}).
			AddRow(uint64(10), uint64(9), uint64(3), "Phone", "Fast", "details"))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT ps.id, ps.sku_code, ps.spec_json, ps.sale_price, i.available_qty, i.version FROM product_skus ps LEFT JOIN inventory i ON i.sku_id = ps.id WHERE ps.product_id = ? AND ps.status = 'ACTIVE' ORDER BY ps.id ASC")).
		WithArgs(uint64(10)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "sku_code", "spec_json", "sale_price", "available_qty", "version"}).
			AddRow(uint64(100), "PHONE-BLACK", `{"color":"black"}`, "99.90", uint64(5), uint64(2)))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, object_key, sort_no FROM product_images WHERE product_id = ? ORDER BY sort_no ASC, id ASC")).
		WithArgs(uint64(10)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "object_key", "sort_no"}).AddRow(uint64(1000), "catalog/phone.jpg", 0))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, rule_type, threshold_amount, discount_amount FROM promotion_rules WHERE product_id = ? AND status = 'ACTIVE' AND start_at <= NOW() AND end_at > NOW() ORDER BY id ASC")).
		WithArgs(uint64(10)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "rule_type", "threshold_amount", "discount_amount"}).AddRow(uint64(20), "THRESHOLD", "100.00", "10.00"))

	repo := NewRepository(db)
	got, err := repo.GetProduct(context.Background(), uint64(10), nil)
	if err != nil {
		t.Fatalf("GetProduct() error = %v", err)
	}
	if got.ID != 10 || len(got.SKUs) != 1 || got.SKUs[0].Inventory.AvailableQty != 5 || len(got.Images) != 1 || len(got.Promotions) != 1 || got.Promotions[0].ThresholdAmount == nil || got.Promotions[0].DiscountAmount == nil {
		t.Fatalf("unexpected product: %#v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRepositoryGetProductAllowsNullableTextFields(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT p.id, p.category_id, p.brand_id, p.title, p.subtitle, p.detail_markdown FROM products p WHERE p.id = ? AND p.status = 'ACTIVE' AND p.deleted_at IS NULL")).
		WithArgs(uint64(12)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "category_id", "brand_id", "title", "subtitle", "detail_markdown"}).
			AddRow(uint64(12), uint64(9), nil, "No Copy", nil, nil))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT ps.id, ps.sku_code, ps.spec_json, ps.sale_price, i.available_qty, i.version FROM product_skus ps LEFT JOIN inventory i ON i.sku_id = ps.id WHERE ps.product_id = ? AND ps.status = 'ACTIVE' ORDER BY ps.id ASC")).
		WithArgs(uint64(12)).WillReturnRows(sqlmock.NewRows([]string{"id", "sku_code", "spec_json", "sale_price", "available_qty", "version"}))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, object_key, sort_no FROM product_images WHERE product_id = ? ORDER BY sort_no ASC, id ASC")).
		WithArgs(uint64(12)).WillReturnRows(sqlmock.NewRows([]string{"id", "object_key", "sort_no"}))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, rule_type, threshold_amount, discount_amount FROM promotion_rules WHERE product_id = ? AND status = 'ACTIVE' AND start_at <= NOW() AND end_at > NOW() ORDER BY id ASC")).
		WithArgs(uint64(12)).WillReturnRows(sqlmock.NewRows([]string{"id", "rule_type", "threshold_amount", "discount_amount"}))

	got, err := NewRepository(db).GetProduct(context.Background(), uint64(12), nil)
	if err != nil {
		t.Fatalf("GetProduct() error = %v", err)
	}
	if got.Subtitle != nil || got.DetailMarkdown != nil {
		t.Fatalf("expected nullable text fields to remain nil: %#v", got)
	}
	if len(got.Promotions) != 0 {
		t.Fatalf("expected no active promotions: %#v", got.Promotions)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRepositoryGetProductMapsNoRowsToNotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT p.id, p.category_id, p.brand_id, p.title, p.subtitle, p.detail_markdown FROM products p WHERE p.id = ? AND p.status = 'ACTIVE' AND p.deleted_at IS NULL")).
		WithArgs(uint64(404)).
		WillReturnError(sql.ErrNoRows)

	repo := NewRepository(db)
	_, err = repo.GetProduct(context.Background(), uint64(404), nil)
	var notFound *NotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("expected NotFoundError, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRepositoryCheckoutSKUsPreservesInputOrderAndUsesOnePromotionQuery(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectQuery(regexp.QuoteMeta("SELECT ps.id, ps.product_id, p.title, ps.sku_code, ps.spec_json, ps.sale_price FROM product_skus ps JOIN products p ON p.id = ps.product_id WHERE ps.id IN (?,?) AND p.status = 'ACTIVE' AND p.deleted_at IS NULL AND ps.status = 'ACTIVE'")).
		WithArgs(uint64(8), uint64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "product_id", "title", "sku_code", "spec_json", "sale_price"}).
			AddRow(uint64(7), uint64(10), "Keyboard", "KB-1", `{"layout":"75%"}`, "99.90").
			AddRow(uint64(8), uint64(11), "Mouse", "MS-1", `{"color":"black"}`, "19.00"))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT product_id, id, rule_type, threshold_amount, discount_amount FROM promotion_rules WHERE product_id IN (?,?) AND status = 'ACTIVE' AND start_at <= NOW(3) AND end_at > NOW(3) ORDER BY product_id ASC, id ASC")).
		WithArgs(uint64(10), uint64(11)).
		WillReturnRows(sqlmock.NewRows([]string{"product_id", "id", "rule_type", "threshold_amount", "discount_amount"}).
			AddRow(uint64(10), uint64(20), "THRESHOLD", "100.00", "10.00"))

	got, err := NewRepository(db).CheckoutSKUs(context.Background(), []uint64{8, 7, 8})
	if err != nil {
		t.Fatalf("CheckoutSKUs() error = %v", err)
	}
	if len(got) != 3 || got[0].SKUID != 8 || got[1].SKUID != 7 || got[2].SKUID != 8 || !got[0].Saleable || len(got[1].Promotions) != 1 {
		t.Fatalf("unexpected checkout snapshots: %#v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRepositoryCheckoutSKUsRejectsMissingOrInvalidDecimalPrice(t *testing.T) {
	for name, rows := range map[string]*sqlmock.Rows{
		"missing sku":     sqlmock.NewRows([]string{"id", "product_id", "title", "sku_code", "spec_json", "sale_price"}),
		"invalid decimal": sqlmock.NewRows([]string{"id", "product_id", "title", "sku_code", "spec_json", "sale_price"}).AddRow(uint64(7), uint64(10), "Keyboard", "KB-1", `{}`, "99.9"),
	} {
		t.Run(name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			mock.ExpectQuery(regexp.QuoteMeta("SELECT ps.id, ps.product_id, p.title, ps.sku_code, ps.spec_json, ps.sale_price FROM product_skus ps JOIN products p ON p.id = ps.product_id WHERE ps.id IN (?) AND p.status = 'ACTIVE' AND p.deleted_at IS NULL AND ps.status = 'ACTIVE'")).WithArgs(uint64(7)).WillReturnRows(rows)
			_, err = NewRepository(db).CheckoutSKUs(context.Background(), []uint64{7})
			if err == nil {
				t.Fatal("CheckoutSKUs() error = nil")
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRepositoryCheckoutSKUsRejectsInactiveSKU(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectQuery(regexp.QuoteMeta("SELECT ps.id, ps.product_id, p.title, ps.sku_code, ps.spec_json, ps.sale_price FROM product_skus ps JOIN products p ON p.id = ps.product_id WHERE ps.id IN (?) AND p.status = 'ACTIVE' AND p.deleted_at IS NULL AND ps.status = 'ACTIVE'")).
		WithArgs(uint64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "product_id", "title", "sku_code", "spec_json", "sale_price"}))

	_, err = NewRepository(db).CheckoutSKUs(context.Background(), []uint64{7})
	var notFound *NotFoundError
	if !errors.As(err, &notFound) || notFound.Resource != "sku" || notFound.ID != 7 {
		t.Fatalf("CheckoutSKUs() error = %v, want inactive SKU NotFoundError", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
