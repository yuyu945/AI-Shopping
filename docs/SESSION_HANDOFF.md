# AI-Shopping Session Handoff

## Current State

- Repository: `D:\简历\AI-Shopping`
- Repository branch: `main`
- M2.1 购物车与不可变订单快照已完成；M2.2 余额支付与库存预留 Saga 已合入主线并通过 baseline 验证。
- M3.1 文档上传与事件可靠投递已完成；M3.2 解析、向量化和版本检索已完成；M4.1 Agent 运行模型与受控 Tool 已完成。下一阶段是 M4.2 推荐二次校验与可回放记录。

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

M2.2 余额支付与库存预留 Saga 已完成：

- `order-service` 在 `trade_db` 中完成 `PENDING_PAYMENT -> PAYMENT_PROCESSING` 支付认领、钱包扣款、钱包流水、订单 `PAID` 状态和 `inventory.reservation.confirm` Outbox 写入；Outbox 写入失败会回滚同一 transaction 内的订单、钱包和流水。
- `product-service` 拥有 `inventory_reservations`，实现 `ReserveStock`、`ConfirmReservation`、`ReleaseReservation` 与 `GetReservation`；库存条件扣减和预留写入在 `catalog_db` transaction 内原子完成，重复请求按 `reservation_id + sku_id` 幂等。
- 全额优惠零金额支付不读取或更新钱包，不写钱包流水；重复支付返回已支付结果或稳定的 `PAYMENT_IN_PROGRESS`。
- 支付恢复 worker 与预留过期 worker 已实现；过期预留会查询订单结算状态，已支付则确认，未支付/取消才释放，依赖超时保留预留并退避重试。
- Order Outbox worker 保留未成功投递记录并退避重试；产品侧确认 consumer 使用 `event_consumptions` 幂等处理确认事件。

M3.1 知识库上传与 Outbox 已完成：

- `knowledge-service` 从启动骨架升级为 zRPC 服务，提供 `UploadDocument` RPC；Gateway 暴露受 JWT 保护的 `POST /api/v1/knowledge/documents` JSON endpoint。
- 上传请求使用 base64 文件内容，支持固定资料类型 `DETAIL`、`SPEC`、`FAQ`、`AFTER_SALE`；服务端从 JWT 派生 `created_by_user_id`，不信任请求体用户 ID。
- 原文件写入 MinIO，`knowledge_documents(status=PENDING)` 与 `knowledge.document.ingest` Outbox 在 `knowledge_db` transaction 内原子写入；重复 `(product_id, doc_type, source_hash)` 返回既有文档。
- knowledge Outbox worker 使用 lease、同步 Kafka ack、退避 retry 和 `DEAD` 终态；未发布成功的事件保留在 `knowledge_db.outbox_events`。
- 新增 `knowledge_db` 的 `knowledge_documents`、`outbox_events`、`event_consumptions`、`embedding_tasks` schema 和 `scripts/apply_knowledge_migrations.ps1`。

M3.2 完成内容：

- 新增 M3.2 设计与实施计划：`docs/superpowers/specs/2026-08-23-m3.2-knowledge-processing-retrieval-design.md`、`docs/superpowers/plans/2026-08-23-m3.2-knowledge-processing-retrieval.md`。
- 新增 `knowledge_chunks` schema、`knowledge_documents.is_current_ready`、`knowledge_documents.ready_at` 和知识库迁移 `20260823_m3_2_knowledge_processing_retrieval.sql`。
- 实现 Chunker：规范化 CRLF/NUL/空行，按 Markdown heading/段落切分，超长段落硬切，保存 section 与 content hash。
- 实现 `IngestService`：消费 `knowledge.document.ingest` payload，以 `document_no` 查回文档，读取 MinIO object，写 Chunk，并创建 `knowledge.chunk.embed` Outbox event。
- 实现 `EmbedService`：调用 `EmbeddingProvider`、`VectorStore`，向量写入成功后在 MySQL transaction 内标记 Chunk `EMBEDDED`、embedding task `DONE`、新文档 `READY/current`，失败只记录 retry，不清理旧 current-ready。
- 实现 `RetrievalService` 和 `SearchProductKnowledge` gRPC：检索前读取 current-ready 文档，向量检索后再用 MySQL 过滤 current-ready Chunk，返回 snippet、doc type、version、section、source page 和 score。
- knowledge-service runtime config 已切换为阿里 DashScope `text-embedding-v4` / `1024` 维，runtime wiring 已接入 DashScope HTTP Embedding adapter、Milvus REST VectorStore adapter，以及 `knowledge.document.ingest` / `knowledge.chunk.embed` Kafka readers。
- 新增 M3.2 gated integration harness：`services/knowledge-service/internal/knowledge/knowledge_integration_test.go`、`integration_config_test.go`、`integration_support_test.go`、`scripts/prepare_knowledge_integration.ps1`、`scripts/test_knowledge_m32_integration.ps1`。普通测试默认跳过；只有 `AI_SHOPPING_KNOWLEDGE_M32_INTEGRATION=1` 运行脚本、且 `AI_SHOPPING_INTEGRATION=1` / `AI_SHOPPING_INTEGRATION_ISOLATED=m32knowledge` / UUID guard 匹配时才连接真实 MySQL、MinIO、Kafka、Milvus 和 DashScope。

M4.1 完成内容：

- 新增 Agent runtime schema 和 migration：`agent_sessions`、`agent_messages`、`agent_runs`、`agent_steps`，状态固定为 `RUNNING` / `SUCCEEDED` / `FAILED` / `TIMEOUT`。
- 新增 `api/agent/agent.proto` 与 generated gRPC contract，`agent-service` 暴露 `StartRun` 和 `GetRun`。
- 实现 MySQL repository、bounded `RunService`、`ChatModel` provider boundary、disabled model adapter、Run/Step terminal persistence 和 stable error taxonomy。
- 实现受控 Tool registry 与 executor：`search_products`、`get_user_profile`、`get_price_stock`、`get_discount`、`search_product_knowledge`。Tool 有 schema、timeout、max result 和权限来源；当前用户 ID 从服务端 JWT 注入，模型提供 `user_id` 会被拒绝。
- 实现 product/user/knowledge gRPC Tool adapters；`search_product_knowledge` 对 `NO_READY_KNOWLEDGE` 等 fallback reason 只做受控返回，不生成资料外断言。
- `services/agent-service/etc/agent-service.yaml` 已补 runtime、ProductRPC、UserRPC、KnowledgeRPC 配置。M4.1 不新增 secret；复用 `AI_SHOPPING_MYSQL_DSN` 和 `AI_SHOPPING_JWT_SECRET`。
- M4.1 不写 `recommendations` 快照；真实 Eino/LLM provider、最终推荐 SKU schema、后端二次校验和可信推荐快照留到 M4.2。

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

Latest baseline re-run on 2026-08-23 from `main`:

- `go test ./... -count=1` passed.
- `go vet ./...` passed.
- `git diff --check` passed.

M4.1 targeted validation on 2026-08-23:

- `go test ./services/agent-service/internal/agent -run 'TestToolRegistry|TestGetUserProfile|TestRepository|TestAgentRuntimeSchemaContract' -count=1` passed.
- `go test ./services/agent-service/internal/agent -run 'Test.*Tool' -count=1` passed.
- `go test ./services/agent-service/internal/agent -run 'TestRunService' -count=1` passed.
- `go test ./services/agent-service/... -run 'TestAgentServer|TestAgentServiceConfig' -count=1` passed.
- `go test ./services/agent-service/... -count=1` passed.

M3.1 targeted validation on 2026-08-23:

- `go test ./services/knowledge-service/... ./apps/gateway/... ./internal/platform/... -count=1` passed.
- Docker Compose configuration validation with temporary local environment values

Real local validation used an isolated Docker Compose project, `m12verify`, with MySQL bound to `127.0.0.1:3307`, a temporary user-service on `9003`, and Gateway on `8889`. It verified two users can register and log in; a JWT can read its own profile; the first address becomes default; an explicit default switch persists; address listing is user-scoped; and a cross-user address delete returns 404. Service logs were checked to ensure the test password was absent.

Cache invalidation validation uses a separate isolated Docker Compose project, `m12cacheverify`, with MySQL on `127.0.0.1:3308` and Redis on `127.0.0.1:6381`. The build-tagged test requires its opt-in environment variables, nonzero Redis DB index, and a UUID `AI_SHOPPING_INTEGRATION_RUN_ID` that is present in the test-only database guard table created by `scripts/prepare_cache_invalidation_integration.ps1`; it must not write without all of those conditions. The test dynamically discovers a seeded product with SKUs and covers both cases: all product/SKU cache keys are immediately removed and persisted tasks reach `DONE`; when a wrapper fails the first immediate Redis `Delete`, that key remains while its task is `PENDING`, then the bounded real worker removes it and all tasks reach `DONE`. Cleanup uses exact inserted task IDs and a product-version CAS before restoring the fixture.

M2.1 真实 MySQL 验证使用 `m21ordersnapshot` 专用 Compose project（MySQL `127.0.0.1:3310`）。`scripts/test_order_snapshot_integration.ps1` 需要显式 `AI_SHOPPING_ORDER_SNAPSHOT_INTEGRATION=1`；否则只输出 `SKIP`。harness 生成 UUID guard，准备脚本只接受健康的 `m21ordersnapshot-mysql-1`，build-tagged test 还要求 `AI_SHOPPING_INTEGRATION=1`、精确 isolation sentinel `m21ordersnapshot`、指向 `trade_db` 的 loopback 非 `3306` DSN 及匹配的 guard row。它在 repository/service integration 层实际覆盖购物车 add/update/delete、非本人地址稳定 `NOT_FOUND`、同一 `request_id` 返回原订单，以及 catalog 标题和价格变更后历史订单不变；不应将其描述为 Gateway HTTP 端到端验证。测试精确清理 fixture，harness 清理 Compose 资源并恢复环境。

The `m12verify` and `m12cacheverify` Docker projects, their volumes, temporary processes, and temporary logs were removed during their respective close-outs.

## Next Milestone

M4.2 后续开发。保持边界：Agent 只能在 `agent-service` 中使用 Eino 和模型 Provider；Tool 必须 typed、受控、有 schema/timeout/max result/权限来源；模型不能直接写订单、库存、余额或执行 SQL。M4.2 应定义模型最终推荐 SKU schema，并在写 `recommendations` 前通过后端 Tool 二次校验价格、库存、优惠和可售状态。

## Integration

Continue directly on `main` per the latest user instruction. Do not rewrite history; push verified milestones as part of the already approved mainline push workflow.
