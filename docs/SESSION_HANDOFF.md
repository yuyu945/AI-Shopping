# AI-Shopping Session Handoff

## Current State

- Repository: `D:\简历\AI-Shopping`
- Transaction worktree: `D:\简历\AI-Shopping\.worktrees\m2-transaction`
- Feature branch: `codex/m2-transaction`
- M2.1 购物车与不可变订单快照已完成；M2.2 已有 Saga 设计，尚未开始实现。

## Completed

M1.1 bootstrap and the following M1.2 paths are complete:

- Product read path: catalog schema and seed data, SKU-aware list/detail reads, promotion mapping, Redis Cache Aside, product gRPC/HTTP APIs, trace propagation, and graceful Redis fallback.
- Product cache invalidation path: a catalog detail update and all affected cache invalidation tasks commit in one `catalog_db` transaction; immediate deletion is best effort; the scheduler/worker leases due or stale tasks, performs delayed deletion, retries with bounded backoff, and records `DONE` or `DEAD` without request-handler sleeps.
- User authentication path: user gRPC contract, `user_db` repository, bcrypt password handling, HS256 JWT (24-hour TTL and `ai-shopping` issuer), protected user-service gRPC methods, Gateway JWT middleware, HTTP handlers/routes, profile updates, and user-owned address CRUD/default-address behavior.
- Security boundaries: Gateway validates protected bearer tokens and forwards the authenticated bearer; user-service verifies it again and derives identity from claims. Address access always scopes by `id + user_id`; a non-owned address returns stable 404. Auth request bodies are excluded from RPC content logs.

M2.1 交易前置路径已完成：

- `order-service` 只写入 `trade_db`，提供用户隔离的购物车增改删、`PENDING_PAYMENT` 幂等建单和只读订单快照展示。
- 地址快照由 `user-service` 的 JWT 受保护接口按当前用户读取；Checkout SKU 快照由 `product-service` 直接查询 MySQL，不经过 Redis 商品详情缓存。
- 订单和订单项持久化地址、商品标题、SKU、规格、价格和优惠快照；MySQL JSON 参数以 JSON document 写入，避免驱动把 JSON 文本保存为 JSON string。

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

M2.1 真实 MySQL 验证使用 `m21ordersnapshot` 专用 Compose project（MySQL `127.0.0.1:3310`）。`scripts/test_order_snapshot_integration.ps1` 需要显式 `AI_SHOPPING_ORDER_SNAPSHOT_INTEGRATION=1`；否则只输出 `SKIP`。harness 生成 UUID guard，准备脚本只接受健康的 `m21ordersnapshot-mysql-1`，build-tagged test 还要求 `AI_SHOPPING_INTEGRATION=1`、精确 isolation sentinel `m21ordersnapshot`、指向 `trade_db` 的 loopback 非 `3306` DSN 及匹配的 guard row。它在 repository/service integration 层实际覆盖购物车 add/update/delete、非本人地址稳定 `NOT_FOUND`、同一 `request_id` 返回原订单，以及 catalog 标题和价格变更后历史订单不变；不应将其描述为 Gateway HTTP 端到端验证。测试精确清理 fixture，harness 清理 Compose 资源并恢复环境。

The `m12verify` and `m12cacheverify` Docker projects, their volumes, temporary processes, and temporary logs were removed during their respective close-outs.

## Next Milestone

M2.2 余额支付与库存预留 Saga。先遵循已审阅的 Saga 设计，再实现产品侧预留、订单侧支付认领、余额流水、Outbox 确认和恢复 worker。保持边界：MySQL 是商品、库存、交易和快照的事实源；Redis 降级不得影响业务事实；服务不得直接访问其他服务 schema。

## Integration

Before integration, re-run the automated validation above. The unpushed local commits on `codex/m1-user-auth` are focused and should be merged into `feat/m1-bootstrap` only after explicit authorization. Do not push without explicit authorization.
