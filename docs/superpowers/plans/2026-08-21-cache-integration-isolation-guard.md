# Cache Integration Isolation Guard Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Prevent the write-enabled cache integration test from operating on an undeclared MySQL instance and make its cleanup target only rows created by that test.

**Architecture:** A disposable Docker preparation script creates a test-only database guard row keyed by a caller-provided UUID. The integration test requires that UUID before it can discover a fixture. The catalog mutation repository returns inserted task IDs, letting the test delete precise rows; the seed product is restored with a version compare-and-set.

**Tech Stack:** Go 1.25, `database/sql`, MySQL 8.4, Redis 7.4, PowerShell, Docker Compose.

---

## File Structure

| File | Responsibility |
| --- | --- |
| `services/product-service/internal/catalog/model.go` | Add transaction-created task IDs to `MutationResult`. |
| `services/product-service/internal/catalog/mutation_repository.go` | Capture MySQL inserted task IDs. |
| `services/product-service/internal/catalog/mutation_repository_test.go` | Assert exact IDs returned from a successful transaction. |
| `services/product-service/internal/catalog/cache_invalidation_integration_config_test.go` | Require a UUID run ID in explicit integration configuration. |
| `services/product-service/internal/catalog/cache_invalidation_integration_test.go` | Validate DB guard and use precise cleanup plus fixture CAS. |
| `scripts/prepare_cache_invalidation_integration.ps1` | Prepare the named disposable MySQL container, seed catalog data, and insert the guard row. |
| `README.md` and cache design spec | Record safe invocation and isolation semantics. |

### Task 1: Return inserted task IDs

**Files:**
- Modify: `services/product-service/internal/catalog/model.go`
- Modify: `services/product-service/internal/catalog/mutation_repository.go`
- Modify: `services/product-service/internal/catalog/mutation_repository_test.go`

- [ ] Write a failing repository test that expects `MutationResult.TaskIDs` to equal the `LastInsertId` values from each invalidation-task insert.
- [ ] Run `go test ./services/product-service/internal/catalog -run TestMutationRepository -count=1` and observe the missing-field failure.
- [ ] Add `TaskIDs []uint64` to `MutationResult`; append each `sql.Result.LastInsertId()` after a successful insert and return those IDs only after commit.
- [ ] Run `gofmt`, the repository tests, and `go vet ./services/product-service/internal/catalog`; commit the focused change.

### Task 2: Require a database-side isolation guard

**Files:**
- Modify: `services/product-service/internal/catalog/cache_invalidation_integration_config_test.go`
- Modify: `services/product-service/internal/catalog/cache_invalidation_integration_test.go`
- Create: `scripts/prepare_cache_invalidation_integration.ps1`

- [ ] Write failing configuration tests for a missing and malformed `AI_SHOPPING_INTEGRATION_RUN_ID`.
- [ ] Run `go test ./services/product-service/internal/catalog -run TestCacheInvalidationIntegrationConfig -count=1` and observe the missing validation failure.
- [ ] Parse UUID run IDs only after the opt-in and project sentinel match. In the tagged test, query the test-only guard table for exactly that UUID before fixture discovery or any mutation. The PowerShell script must validate its UUID argument, create the guard table in the named disposable container, seed the catalog, then insert the guard row.
- [ ] Run the configuration tests and the tagged test without environment variables to confirm it still skips safely.

### Task 3: Make integration cleanup exact and conflict-safe

**Files:**
- Modify: `services/product-service/internal/catalog/cache_invalidation_integration_test.go`

- [ ] Write a failing focused test or helper assertion that records exact `TaskIDs` and requires the restore update to affect exactly one row under the expected post-test version.
- [ ] Run the focused catalog tests and observe the helper/API failure.
- [ ] Collect task IDs from each successful mutation; delete by each exact ID in cleanup. Track the expected product version and detail after every successful mutation, and restore the original fixture only with `WHERE id = ? AND version = ? AND detail_markdown = ?`; report a cleanup error when the CAS misses.
- [ ] Run the tagged integration test in a fresh isolated Compose project, verify both compensation paths, `cache_tasks=0`, seed version/detail restoration, Redis DB 15 size zero, and remove containers, volumes, and network.

### Task 4: Document and close out

**Files:**
- Modify: `README.md`
- Modify: `docs/superpowers/specs/2026-08-21-m1.2-cache-invalidation-design.md`
- Modify: `docs/SESSION_HANDOFF.md`

- [ ] Document the run ID generation and preparation script, and state that a database-side guard is required before writes.
- [ ] Run `go test ./... -count=1`, `go vet ./...`, `docker compose -f deploy/docker-compose.yml config -q`, and `git diff --check`.
- [ ] Review the final diff for credentials and scope, commit the verified remediation, and retain the unpushed branch for explicit integration choice.

## Plan Self-Review

- The plan addresses the two review findings: DB identity is no longer asserted solely by an environment string, and cleanup no longer selects rows by broad key/watermark predicates.
- The guard table is created only in the disposable verification database; it is not an application runtime dependency or a production schema migration.
- The plan adds no public API, Redis wildcard deletion, or transaction behavior unrelated to test observability.
