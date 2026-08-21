package catalog

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

const (
	loadProductSKUsQuery        = "SELECT id FROM product_skus WHERE product_id = ? ORDER BY id ASC FOR UPDATE"
	updateProductDetailQuery    = "UPDATE products SET detail_markdown = ?, version = version + 1 WHERE id = ? AND deleted_at IS NULL"
	insertInvalidationTaskQuery = "INSERT INTO cache_invalidation_tasks (cache_key, execute_at, status) VALUES (?, ?, 'PENDING')"
)

// MutationRepository persists catalog detail changes with their cache invalidation tasks.
type MutationRepository struct {
	db *sql.DB
}

// NewMutationRepository constructs a repository for transactional catalog mutations.
func NewMutationRepository(db *sql.DB) *MutationRepository {
	return &MutationRepository{db: db}
}

// UpdateProductDetailAndCreateTasks updates a product detail and schedules every affected cache key atomically.
func (r *MutationRepository) UpdateProductDetailAndCreateTasks(ctx context.Context, productID uint64, detailMarkdown string, executeAt time.Time) (MutationResult, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return MutationResult{}, errors.New("begin product detail update failed")
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.QueryContext(ctx, loadProductSKUsQuery, productID)
	if err != nil {
		return MutationResult{}, errors.New("load product SKUs failed")
	}
	cacheKeys := []string{ProductCacheKey(productID, nil)}
	for rows.Next() {
		var skuID uint64
		if err := rows.Scan(&skuID); err != nil {
			rows.Close()
			return MutationResult{}, errors.New("load product SKUs failed")
		}
		cacheKeys = append(cacheKeys, ProductCacheKey(productID, &skuID))
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return MutationResult{}, errors.New("load product SKUs failed")
	}
	if err := rows.Close(); err != nil {
		return MutationResult{}, errors.New("load product SKUs failed")
	}

	result, err := tx.ExecContext(ctx, updateProductDetailQuery, detailMarkdown, productID)
	if err != nil {
		return MutationResult{}, errors.New("update product detail failed")
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return MutationResult{}, errors.New("update product detail failed")
	}
	if affected == 0 {
		return MutationResult{}, &NotFoundError{Resource: "product", ID: productID}
	}

	taskIDs := make([]uint64, 0, len(cacheKeys))
	for _, cacheKey := range cacheKeys {
		taskResult, err := tx.ExecContext(ctx, insertInvalidationTaskQuery, cacheKey, executeAt)
		if err != nil {
			return MutationResult{}, errors.New("create cache invalidation task failed")
		}
		taskID, err := taskResult.LastInsertId()
		if err != nil || taskID <= 0 {
			return MutationResult{}, errors.New("read cache invalidation task ID failed")
		}
		taskIDs = append(taskIDs, uint64(taskID))
	}
	if err := tx.Commit(); err != nil {
		return MutationResult{}, errors.New("commit product detail update failed")
	}
	return MutationResult{ProductID: productID, DetailMarkdown: detailMarkdown, CacheKeys: cacheKeys, TaskIDs: taskIDs}, nil
}
