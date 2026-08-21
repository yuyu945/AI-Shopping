# AI-Shopping Session Handoff

## Current State

- Repository: `D:\简历\AI-Shopping`
- Authentication worktree: `D:\简历\AI-Shopping\.worktrees\m1-user-auth`
- Feature branch: `codex/m1-user-auth`, based on `feat/m1-bootstrap`
- The branch contains the completed M1.2 user authentication path and its documentation close-out. It has not been pushed or merged.

## Completed

M1.1 bootstrap and the following M1.2 paths are complete:

- Product read path: catalog schema and seed data, SKU-aware list/detail reads, promotion mapping, Redis Cache Aside, product gRPC/HTTP APIs, trace propagation, and graceful Redis fallback.
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

The `m12verify` Docker project, its volume, temporary processes, and temporary logs were removed during this close-out.

## Remaining M1.2 Work

Do not mark the whole M1.2 milestone complete yet. The scheduler-driven delayed second cache invalidation and retry path remains unimplemented:

1. Persist `cache_invalidation_tasks` after catalog writes.
2. Implement a scheduler/worker retry flow without sleeping in request handlers.
3. Add tests for immediate deletion failure, delayed second deletion, retry, and confirmation that inventory remains independent of Redis.

After that, continue with M2 transaction work in `docs/TASKS.md`. Preserve the existing boundary: MySQL is the source of truth; Redis is never an inventory or payment fact source; services do not directly access another service's schema.

## Integration

Before integration, re-run the automated validation above. The unpushed local commits on `codex/m1-user-auth` are focused and should be merged into `feat/m1-bootstrap` only after explicit authorization. Do not push without explicit authorization.
