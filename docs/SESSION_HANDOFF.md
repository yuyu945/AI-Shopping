# AI-Shopping Session Handoff

## Current State

- Repository: `D:\简历\AI-Shopping`
- Remote: `origin` -> `git@github.com:yuyu945/AI-Shopping.git`
- Working branch: `feat/m1-bootstrap`
- Latest completed feature commit: `0a603ff docs: close M1.2 product read path`
- The branch has been pushed to `origin/feat/m1-bootstrap`.

## Completed

M1.1 bootstrap and the product-read portion of M1.2 are complete:

- Docker Compose dependencies and logical MySQL schemas.
- Catalog schema, idempotent seed data, repository queries, SKU-aware detail reads.
- Product application service with Redis Cache Aside. MySQL remains the source of product, price, and inventory facts.
- Redis startup failure degrades to a nil cache instead of blocking product-service startup.
- Product gRPC contract and Gateway HTTP routes:
  - `GET /api/v1/products`
  - `GET /api/v1/products/{id}`
- Keyword/category/pagination filters, optional SKU selection, active promotion mapping, stable HTTP errors, and trace propagation.
- Application DTOs are separated from persistence models.
- Idempotent seed command: `scripts/seed_catalog.ps1`.

Relevant documents:

- `README.md`
- `docs/PLAN.md`
- `docs/TASKS.md`
- `docs/superpowers/plans/2026-08-21-m1.2-product-read-path.md`
- `deploy/mysql/seed/02-catalog-seed.sql`

## Verification

- `go test ./... -count=1` passed.
- `go vet ./...` passed.
- `git diff --check` passed.
- Docker Compose config validation passed.
- Real Docker MySQL/Redis/Gateway verification passed for list, keyword filter, detail, SKU selection, promotion, inventory, trace header, Redis cache keys, and not-found response.
- Seed verification returned 4 products, 7 SKUs, and 1 promotion.

## Next Work

1. Read `AGENTS.md`, `docs/PLAN.md`, `docs/TASKS.md`, and the M1.2 plan.
2. Complete the remaining M1.2 user path: registration/login, JWT, identity middleware, user profile/address ownership checks.
3. Keep boundaries: Gateway HTTP, service-to-service gRPC, no cross-service database access, Redis is never an inventory fact source.
4. Create a focused commit after each verified milestone. Push only when explicitly requested.

## Runtime Notes

- The host Redis port may be `6380` when `6379` is occupied; the application address must match.
- The default product-service config uses etcd at `127.0.0.1:2379`. Host-side direct testing needs that port exposed, or a temporary RPC config without an `Etcd` section.
- Never commit real credentials, tokens, or DSNs.

## Suggested Skills

- `fullstack-developer`
- `superpowers:writing-plans`
- `superpowers:test-driven-development`
- `superpowers:verification-before-completion`
- `fix`
