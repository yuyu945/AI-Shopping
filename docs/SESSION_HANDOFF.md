# AI-Shopping Session Handoff

## Current State

- Repository: `D:\简历\AI-Shopping`
- Authentication worktree: `D:\简历\AI-Shopping\.worktrees\m1-user-auth`
- Feature branch: `codex/m1-user-auth`, based on `feat/m1-bootstrap`
- The branch contains the completed M1.2 user authentication, product read, and durable cache invalidation paths. It has not been pushed or merged.

## Completed

M1.1 bootstrap and the following M1.2 paths are complete:

- Product read path: catalog schema and seed data, SKU-aware list/detail reads, promotion mapping, Redis Cache Aside, product gRPC/HTTP APIs, trace propagation, and graceful Redis fallback.
- Product cache invalidation path: a catalog detail update and all affected cache invalidation tasks commit in one `catalog_db` transaction; immediate deletion is best effort; the scheduler/worker leases due or stale tasks, performs delayed deletion, retries with bounded backoff, and records `DONE` or `DEAD` without request-handler sleeps.
- User authentication path: user gRPC contract, `user_db` repository, bcrypt password handling, HS256 JWT (24-hour TTL and `ai-shopping` issuer), protected user-service gRPC methods, Gateway JWT middleware, HTTP handlers/routes, profile updates, and user-owned address CRUD/default-address behavior.
- Security boundaries: Gateway validates protected bearer tokens and forwards the authenticated bearer; user-service verifies it again and derives identity from claims. Address access always scopes by `id + user_id`; a non-owned address returns stable 404. Auth request bodies are excluded from RPC content logs.

Key endpoints:

- `POST /api/v1/auth/register`
- `POST /api/v1/auth/login`
- `GET /api/v1/users/me`
- `PUT /api/v1/users/me/profile`
- `GET|POST /api/v1/users/me/addresses`
- `PUT|DELETE /api/v1/users/me/addresses/{id}`

`AI_SHOPPING_JWT_SECRET` is required by Gateway and user-service, must be at least 32 bytes, and must only be supplied through the local environment or secret management.

## Verification

Automated validation completed for this branch:

- `go test ./... -count=1`
- `go vet ./...`
- `git diff --check`
- Docker Compose configuration validation with temporary local environment values

Real local validation used an isolated Docker Compose project, `m12verify`, with MySQL bound to `127.0.0.1:3307`, a temporary user-service on `9003`, and Gateway on `8889`. It verified two users can register and log in; a JWT can read its own profile; the first address becomes default; an explicit default switch persists; address listing is user-scoped; and a cross-user address delete returns 404. Service logs were checked to ensure the test password was absent.

Cache invalidation validation uses a separate isolated Docker Compose project, `m12cacheverify`, with MySQL on `127.0.0.1:3308` and Redis on `127.0.0.1:6381`. The build-tagged test requires its opt-in environment variables, nonzero Redis DB index, and a UUID `AI_SHOPPING_INTEGRATION_RUN_ID` that is present in the test-only database guard table created by `scripts/prepare_cache_invalidation_integration.ps1`; it must not write without all of those conditions. The test dynamically discovers a seeded product with SKUs and covers both cases: all product/SKU cache keys are immediately removed and persisted tasks reach `DONE`; when a wrapper fails the first immediate Redis `Delete`, that key remains while its task is `PENDING`, then the bounded real worker removes it and all tasks reach `DONE`. Cleanup uses exact inserted task IDs and a product-version CAS before restoring the fixture.

The `m12verify` and `m12cacheverify` Docker projects, their volumes, temporary processes, and temporary logs were removed during their respective close-outs.

## Next Milestone

M1.2 is complete. Continue with M2 transaction work in `docs/TASKS.md`: shopping cart and immutable order snapshots first, then idempotent balance payment, conditional inventory deduction, wallet ledger, and transactional Outbox. Preserve the existing boundary: MySQL is the source of truth for products, inventory, cache invalidation tasks, and transactions; Redis degradation may reduce cache performance but must not change committed business facts; services do not directly access another service's schema.

## Integration

Before integration, re-run the automated validation above. The unpushed local commits on `codex/m1-user-auth` are focused and should be merged into `feat/m1-bootstrap` only after explicit authorization. Do not push without explicit authorization.
