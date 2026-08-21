package catalog

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestMutationRepositoryUpdateProductDetailAndCreateTasksCommitsAllCacheKeys(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	executeAt := time.Date(2026, time.August, 21, 9, 30, 0, 0, time.UTC)
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id FROM product_skus WHERE product_id = ? ORDER BY id ASC FOR UPDATE")).
		WithArgs(uint64(10)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uint64(100)).AddRow(uint64(101)))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE products SET detail_markdown = ?, version = version + 1 WHERE id = ? AND deleted_at IS NULL")).
		WithArgs("updated detail", uint64(10)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	insertTask := regexp.QuoteMeta("INSERT INTO cache_invalidation_tasks (cache_key, execute_at, status) VALUES (?, ?, 'PENDING')")
	mock.ExpectExec(insertTask).WithArgs(ProductCacheKey(10, nil), executeAt).WillReturnResult(sqlmock.NewResult(1, 1))
	for _, skuID := range []uint64{100, 101} {
		skuID := skuID
		mock.ExpectExec(insertTask).WithArgs(ProductCacheKey(10, &skuID), executeAt).WillReturnResult(sqlmock.NewResult(1, 1))
	}
	mock.ExpectCommit()

	got, err := NewMutationRepository(db).UpdateProductDetailAndCreateTasks(context.Background(), 10, "updated detail", executeAt)
	if err != nil {
		t.Fatalf("UpdateProductDetailAndCreateTasks() error = %v", err)
	}
	wantKeys := []string{ProductCacheKey(10, nil), ProductCacheKey(10, uint64Ptr(100)), ProductCacheKey(10, uint64Ptr(101))}
	if got.ProductID != 10 || got.DetailMarkdown != "updated detail" || !sameStrings(got.CacheKeys, wantKeys) {
		t.Fatalf("UpdateProductDetailAndCreateTasks() = %#v, want product detail and keys %#v", got, wantKeys)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMutationRepositoryUpdateProductDetailAndCreateTasksRollsBackNotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id FROM product_skus WHERE product_id = ? ORDER BY id ASC FOR UPDATE")).
		WithArgs(uint64(404)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE products SET detail_markdown = ?, version = version + 1 WHERE id = ? AND deleted_at IS NULL")).
		WithArgs("missing", uint64(404)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()

	_, err = NewMutationRepository(db).UpdateProductDetailAndCreateTasks(context.Background(), 404, "missing", time.Now())
	var notFound *NotFoundError
	if !errors.As(err, &notFound) || notFound.Resource != "product" || notFound.ID != 404 {
		t.Fatalf("UpdateProductDetailAndCreateTasks() error = %v, want product NotFoundError", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMutationRepositoryUpdateProductDetailAndCreateTasksRollsBackTaskFailureWithoutDriverDetails(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	executeAt := time.Date(2026, time.August, 21, 9, 30, 0, 0, time.UTC)
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id FROM product_skus WHERE product_id = ? ORDER BY id ASC FOR UPDATE")).
		WithArgs(uint64(10)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uint64(100)))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE products SET detail_markdown = ?, version = version + 1 WHERE id = ? AND deleted_at IS NULL")).
		WithArgs("updated detail", uint64(10)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO cache_invalidation_tasks (cache_key, execute_at, status) VALUES (?, ?, 'PENDING')")).
		WithArgs(ProductCacheKey(10, nil), executeAt).
		WillReturnError(errors.New("task insert unavailable"))
	mock.ExpectRollback()

	_, err = NewMutationRepository(db).UpdateProductDetailAndCreateTasks(context.Background(), 10, "updated detail", executeAt)
	if err == nil {
		t.Fatal("UpdateProductDetailAndCreateTasks() error = nil")
	}
	if strings.Contains(err.Error(), "task insert unavailable") {
		t.Fatalf("UpdateProductDetailAndCreateTasks() leaked driver detail: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMutationRepositoryUpdateProductDetailAndCreateTasksCommitFailureDoesNotReturnSuccessfulMutation(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	executeAt := time.Date(2026, time.August, 21, 9, 30, 0, 0, time.UTC)
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id FROM product_skus WHERE product_id = ? ORDER BY id ASC FOR UPDATE")).
		WithArgs(uint64(10)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uint64(100)))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE products SET detail_markdown = ?, version = version + 1 WHERE id = ? AND deleted_at IS NULL")).
		WithArgs("updated detail", uint64(10)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	insertTask := regexp.QuoteMeta("INSERT INTO cache_invalidation_tasks (cache_key, execute_at, status) VALUES (?, ?, 'PENDING')")
	mock.ExpectExec(insertTask).WithArgs(ProductCacheKey(10, nil), executeAt).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(insertTask).WithArgs(ProductCacheKey(10, uint64Ptr(100)), executeAt).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit().WillReturnError(errors.New("commit unavailable"))

	got, err := NewMutationRepository(db).UpdateProductDetailAndCreateTasks(context.Background(), 10, "updated detail", executeAt)
	if err == nil {
		t.Fatal("UpdateProductDetailAndCreateTasks() error = nil")
	}
	if strings.Contains(err.Error(), "commit unavailable") {
		t.Fatalf("UpdateProductDetailAndCreateTasks() leaked driver detail: %v", err)
	}
	if got.ProductID != 0 || got.DetailMarkdown != "" || len(got.CacheKeys) != 0 {
		t.Fatalf("UpdateProductDetailAndCreateTasks() returned successful mutation: %#v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func uint64Ptr(value uint64) *uint64 {
	return &value
}

func sameStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
