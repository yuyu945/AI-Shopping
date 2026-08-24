# 智选购 MVP 实施任务清单

## 1. 使用方式

- 当前阶段：M5 已完成；下一里程碑为 M6 可靠性验证与交付收口。
- 任务按依赖顺序执行；每完成一个里程碑，先运行其验证命令、检查文档同步，再创建一个聚焦 commit。
- 所有实现必须遵守 [AGENTS.md](../AGENTS.md)、[PRD.md](PRD.md)、[architecture.md](architecture.md)、[interaction.md](interaction.md) 和 [MVP 设计文档](智选购-ai导购-mvp-design.md)。设计与实现不一致时，先更新设计并说明取舍。
- 状态标记：`[ ]` 未开始，`[-]` 进行中，`[x]` 已完成，`[!]` 受阻。M1.1、M1.2、M2.1、M2.2、M3.1、M3.2、M4.1、M4.2、M5.1、M5.2 已完成；M6 尚未开始。

## 2. 里程碑总览

| 里程碑 | 目标 | 前置依赖 | 完成定义 |
| --- | --- | --- | --- |
| M1 | 工程骨架与商品读链路 | 无 | Docker Compose 启动基础依赖，用户和商品 API 可用 |
| M2 | 交易闭环与一致性 | M1 | 购物车、幂等建单、余额支付、库存条件更新可验证 |
| M3 | 异步知识库与 RAG | M1 | 文档可异步入库、版本切换、检索带来源 |
| M4 | Agent 导购与推荐校验 | M1、M3 | Agent 可运行、Tool 受控、推荐快照可信 |
| M5 | 用户交互、运营排障与事件分析 | M2、M4 | SSE、最小运营页、评价/行为事件可演示 |
| M6 | 可靠性验证与交付收口 | M2-M5 | 主链路和故障路径测试完整、文档可用于演示与面试 |

## 3. M1：工程骨架与商品读链路

### M1.1 初始化工程与本地依赖（已完成）

- [x] 创建 Go workspace、Go-zero API Gateway 与 `user-service`、`product-service`、`order-service`、`knowledge-service`、`agent-service` 的最小 gRPC 服务骨架。
- [x] 建立统一配置加载、环境变量校验、请求 `trace_id` 透传、HTTP/gRPC 错误码和结构化日志基础设施。
- [x] 编写 Docker Compose，启动 MySQL、Redis、Kafka、Milvus、MinIO 与开发所需 Worker；真实凭证只来自本地 `.env`，`.env.example` 仅列变量名和非敏感示例。
- [x] 创建五个 MySQL schema 与迁移机制：`user_db`、`catalog_db`、`trade_db`、`agent_db`、`knowledge_db`。

验收：空环境执行启动命令后，依赖健康检查成功；缺少必填环境变量时服务以明确错误退出，不输出敏感配置。

### M1.2 用户、商品与缓存读路径（已完成）

- [x] 实现注册登录、JWT、中间件身份注入、用户画像和地址的归属校验。
- [x] 实现分类、品牌、SPU、SKU、图片、库存与优惠的 schema、迁移和演示种子数据。
- [x] 实现商品列表、SKU 详情、关键词/分类筛选，以及商品读 gRPC/HTTP DTO。
- [x] 实现 Cache Aside 商品详情缓存：缓存未命中读 MySQL 后回填；Redis 不可用时回源 MySQL。
- [x] 实现 scheduler 执行延迟二次删除与失败重试，禁止在 HTTP 请求中 sleep。

测试：unit tests 覆盖商品筛选、SKU 切换、缓存未命中/命中、Redis 降级、提交后删除失败、worker lease/退避/`DEAD` 终态；真实 MySQL/Redis integration test 覆盖全商品与 SKU key 的立即删除、持久化 task、延迟删除，以及首次删除失败后由 `PENDING` 收敛为 `DONE`。库存仍以 MySQL 为事实源，M2 的扣减逻辑不得依赖 Redis。

## 4. M2：交易闭环与一致性

### M2.1 购物车与订单快照（已完成）

- [x] 创建购物车、购物车项、订单和订单项迁移，金额字段统一为 `DECIMAL(12,2)`。
- [x] 实现购物车增改删和用户隔离。
- [x] 为建单新增受保护的地址快照与 Checkout SKU Snapshot gRPC 契约：地址由 `user-service` 从 JWT 校验归属；商品由 `product-service` 直读 MySQL 返回 SKU 上架状态、价格、规格和优惠快照，禁止通过订单服务直连其他 schema 或复用 Redis 商品详情缓存。
- [x] 实现创建订单 API：校验地址和购物车，按 `(user_id, request_id)` 生成幂等的 `PENDING_PAYMENT` 订单，写商品、规格、价格、优惠和地址快照。
- [x] 对订单列表与详情只使用订单快照展示，不回填当前商品字段。

测试：unit tests 覆盖空购物车、非本人地址、重复 `request_id`、商品下架和订单快照；真实 MySQL repository/service integration 覆盖购物车增改删、非本人地址、重复 `request_id` 重放和商品变更后的订单快照不变。

### M2.2 余额支付与库存预留 Saga（已完成）

- [x] 创建钱包账户、钱包流水、支付尝试、库存预留、Outbox 和消费幂等迁移，金额字段统一为 `DECIMAL(12,2)`。
- [x] 在订单本地 transaction 中用 `PENDING_PAYMENT -> PAYMENT_PROCESSING` 原子认领支付，并持久化 `payment_attempt_id`、`reservation_id` 与开始时间；重复支付返回已支付结果或稳定的 `PAYMENT_IN_PROGRESS`。
- [x] 由 `product-service` 实现 `ReserveStock`、`ConfirmReservation`、`ReleaseReservation`：在 `catalog_db` transaction 内按 SKU 条件更新库存并写 `inventory_reservations`，任一 SKU 失败时全部回滚。`reservation_id + sku_id` 保障重试幂等。
- [x] 在 `trade_db` transaction 内锁定匹配支付尝试：`total_amount > 0` 时才锁定、校验并更新钱包，且写入 debit 钱包流水；`total_amount == 0` 时不得查询或更新钱包、不得写入流水。两种金额路径都必须在同一 transaction 更新订单为 `PAID`、写入 `inventory.reservation.confirm` Outbox；Outbox 写入失败必须使订单、钱包（若适用）和流水（若适用）全部 rollback。该 transaction 不得访问 `catalog_db`。
- [x] 实现支付恢复与预留过期 worker：过期预留必须查询订单结算状态，已支付则确认，未支付/取消才释放；依赖超时进入退避重试，不能猜测性释放。
- [x] 实现 Outbox Worker 扫描、发布和失败退避，保留未成功投递记录；产品侧 consumer 使用 `event_consumptions` 幂等确认预留。

测试：部分 SKU 库存不足导致整个预留回滚、余额不足后库存最终释放、重复支付、并发支付、支付 transaction 回滚、全额优惠零金额订单不查询/更新钱包且不写钱包流水、Outbox 写入失败时订单/钱包/流水全部回滚、确认事件首次失败后补偿成功、支付完成但确认延迟、预留过期时 order-service 超时、进程崩溃后的 `PAYMENT_PROCESSING` 恢复。验收时查询余额、库存、预留、订单、流水和 Outbox，确认不存在重复扣款、超卖或永久悬挂预留。

## 5. M3：异步知识库与 RAG

### M3.1 文档上传与事件可靠投递（已完成）

- [x] 实现资料上传至 MinIO 和 `knowledge_documents` 记录，支持 DETAIL、SPEC、FAQ、AFTER_SALE 类型。
- [x] 上传 transaction 内写入 `knowledge.document.ingest` Outbox 事件，接口立即返回文档号、版本和 `PENDING` 状态。
- [x] 建立 `outbox_events`、`event_consumptions`、`embedding_tasks` 迁移和通用事件信封，至少包含 `event_id`、事件类型、聚合 ID 和 payload 版本。
- [x] 配置知识库 Outbox 退避与 `DEAD` 终态；重试保留原始事件 ID 和失败分类。

测试：同一文档重复上传、同一事件重复投递、发布失败重试、超过阈值进入 dead-letter。

### M3.2 解析、向量化和版本检索（已完成）

- [x] 创建 `knowledge_chunks` schema、current-ready 字段与迁移，使用 `(document_id, chunk_index)`、`(document_id, content_hash)` 保障 Chunk 幂等。
- [x] 实现解析/规范化/切分 domain，支持 `text/plain`、`text/markdown`、`application/json` 文本资料。
- [x] 实现 `knowledge.document.ingest` 处理服务：以 `document_no` 查回文档，读取 MinIO 原文件，写 Chunk，并创建 `knowledge.chunk.embed` Outbox event。
- [x] 实现 `knowledge.chunk.embed` 处理服务：调用 EmbeddingProvider、VectorStore，并在向量写入成功后切换当前 `READY` 版本；失败不隐藏旧版本。
- [x] 实现商品知识检索 domain、MySQL current-ready 二次过滤和 `SearchProductKnowledge` gRPC。
- [x] 实现真实 Kafka reader runtime wiring、阿里 DashScope `text-embedding-v4` Embedding adapter、Milvus REST VectorStore adapter。
- [x] 补显式 gated integration tests：默认跳过；本地 DashScope API Key、Milvus、Kafka、MinIO、MySQL 都可用且隔离 guard 匹配时才运行。

测试：Chunk 重复消费、Embedding 失败后重试、旧版本过滤、资料无依据问答的受控降级。

## 6. M4：Agent 导购与推荐校验

### M4.1 Agent 运行模型与受控 Tool（已完成）

- [x] 创建会话、消息、`agent_runs`、`agent_steps` 迁移，固定 Run/Step 状态机、索引和可回放时间线；推荐快照由 M4.2 补齐。
- [x] 建立 `ChatModel` provider boundary 和 disabled model adapter；真实 Eino/LLM provider 留到后续阶段，业务服务不依赖模型 SDK。
- [x] 实现 `search_products`、`get_user_profile`、`get_price_stock`、`get_discount`、`search_product_knowledge` Typed Tool，并为每个 Tool 配置输入 Schema、超时、最大返回数量与授权来源。
- [x] 限制每次 Run 最多 8 个 Step、总超时 30 秒；所有外部调用使用带取消与超时的 `context.Context`。

测试：无效 Tool 参数、伪造用户 ID、Tool 超时、超过最大 Step、模型供应商错误；每种失败均落为明确 Run/Step 状态并返回稳定错误码。

### M4.2 推荐二次校验与可回放记录（已完成）

- [x] 定义模型最终输出 Schema，仅接受 SKU ID、排序和理由；未知字段、重复 SKU、重复排序和空理由都会被拒绝。
- [x] 在写 `recommendations` 前通过 product-service 后端快照重查商品、价格、规格、优惠和可售状态，过滤无效 SKU，生成不可变快照。
- [x] 持久化脱敏的模型/Tool 入参出参、耗时、异常、模型版本、Prompt 版本和 `trace_id`；推荐快照只保存后端 `VERIFIED` 数据。
- [x] 实现查询 Run 详情 gRPC，返回最终推荐、校验状态和按步骤排序的时间线数据。

测试：覆盖模型返回不存在 SKU、不可售 SKU、伪造价格字段、重复 SKU/排序、后端二次校验全部失败、推荐持久化和 `GetRun` replay；确认最终推荐只包含后端校验成功的快照。真实 Eino/LLM provider 和 Gateway HTTP/SSE 用户体验留到后续阶段。

## 7. M5：用户交互、运营排障与事件分析

### M5.1 用户端主体验

#### M5.1a Gateway Agent HTTP/SSE 接入（已完成）

- [x] 暴露 `POST /api/v1/agent/runs`、`GET /api/v1/agent/runs/{run_id}` 和 `GET /api/v1/agent/runs/{run_id}/events`。
- [x] Gateway 透传 JWT bearer 与合法 `trace_id`，并对 Agent gRPC 错误做稳定 HTTP taxonomy。
- [x] SSE 以 `GetRun` 持久化状态生成 replay/polling events；真实 live streaming 与真实 LLM provider 留到后续阶段。

#### M5.1b 商品知识问答 Gateway 接入（已完成）

- [x] 暴露 `POST /api/v1/products/{product_id}/knowledge/questions`，通过 Gateway 调用 `SearchProductKnowledge`。
- [x] 返回带来源的 snippets 与 `fallback_reason`，不生成无来源答案文本。
- [x] 保留真实前端商品详情页与自然语言答案生成到后续阶段。

- [x] 实现商品列表、商品详情、SKU 切换、购物车、订单确认、余额支付、订单详情和评价页面，状态遵循 [interaction.md](interaction.md)。
- [x] 实现 AI 导购页：发送自然语言需求、建立 SSE 订阅、渲染 Run 进度、推荐卡片和受控失败提示。
- [x] 实现商品知识问答及来源展示；RAG 不可用时呈现受控降级，不生成无来源断言。
- [x] 实现支付提交中、库存不足、余额不足、网络超时和重复提交后的订单状态回查。

验收：从自然语言需求到推荐、商品详情、购物车、建单和余额支付形成可操作闭环；文本、错误与按钮状态不重叠，移动端关键路径可用。

### M5.2 运营端最小排障能力与事件消费

#### M5.2a 评价事件 Outbox 接入（已完成）

- [x] 支持用户对本人已支付订单项提交一次评价，评价写入 `trade_db.reviews`。
- [x] 同一 transaction 写入 `review.events` Outbox，失败时评价与事件一起回滚。
- [x] Gateway 暴露 `POST /api/v1/orders/{order_no}/items/{sku_id}/reviews`；Kafka 发布和分析消费留到后续阶段。

#### M5.2b Review Events Kafka Analytics（已完成）

- [x] `order-service` Outbox worker 支持发布 `review.events` 到 Kafka，保留库存确认事件发布语义。
- [x] `review.events` consumer 使用 `(event_id, consumer_group)` 幂等，写入最小评价事件明细和商品评分统计。
- [x] 畸形评价事件进入 `review.events.deadletter`；行为事件和运营 UI 留到后续阶段。

#### M5.2c Operations and Behavior Events（已完成）

- [x] 实现资料列表、上传、版本详情、处理状态、失败原因和失败文档重试操作；ops 路由统一位于 `/api/v1/ops/knowledge/*`。
- [x] 实现 Agent Run 列表、状态筛选、Run 详情、Step 时间线、`trace_id` 展示与 `agent-service` 侧脱敏详情。
- [x] 实现用户行为 `behavior.events`，通过 Gateway behavior Outbox 投递 Kafka；评价 `review.events` 继续由 order-service Outbox 投递。
- [x] 实现统计 consumer 的幂等记录、retry/dead-letter 处理和 `/api/v1/ops/events/overview` 最小只读事件概览。
- [x] 实现 React/Vite 运营页：`#/ops/documents`、`#/ops/agent-runs`、`#/ops/events`；ops client 仅在运营调用中发送 `X-AI-Shopping-Operator: true`。

测试：运营账户权限、非本人资源访问、Kafka 重复消费、retry 重放、dead-letter 记录；验证日志与页面不显示敏感数据。

## 8. M6：可靠性验证与交付收口

### M6.1 自动化验证

- [ ] 为核心业务补齐 table-driven unit tests 和 service-level integration tests。
- [ ] 在可启动依赖环境中跑订单、缓存、Agent、RAG、Kafka 的集成测试矩阵。
- [ ] 运行 `gofmt`、受影响包 `go test`、`go vet` 和前端 lint/typecheck；记录命令、环境与结果。
- [ ] 建立演示数据重置脚本和健康检查，确保演示前可重复初始化。

最低测试矩阵：

| 范围 | 必测场景 |
| --- | --- |
| 订单 | 库存不足、余额不足、重复建单、重复支付、transaction 回滚 |
| 缓存 | 未命中回填、首次删除失败、延迟二删、库存不依赖缓存 |
| Agent | Tool 入参错误、超时、Step 上限、无效 SKU、二次校验失败 |
| RAG | 重复投递、版本切换、Embedding 重试、旧版本不召回 |
| Kafka | 重复消费、retry、dead-letter、重放后幂等 |

### M6.2 演示、文档和面试收口

- [ ] 用一条真实演示路径录制或截图：自然语言需求、Agent 时间线、后端快照、建单、余额支付、资料上传与 RAG 来源。
- [ ] 更新 README、环境变量示例、API 文档、数据库迁移说明、Kafka Topic 合约和故障排查说明。
- [ ] 基于固定机器、数据量、并发、预热状态和命令执行可重复压测；未获得证据前不在简历或文档填入性能数字。
- [ ] 审核日志、截图、种子数据和提交历史，确认不存在 API Key、Token、密码、真实地址或手机号。

验收：从空环境搭建、主链路演示、故障用例到排障文档均可复现；所有公开结论能追溯到测试或实际运行证据。

## 9. 实施纪律与退出条件

每个里程碑完成前必须同时满足：

1. 关联的 schema、API、Topic、状态机和服务边界已经同步更新到设计文档。
2. 新增正常、边界、失败分支均有测试或有记录的手动集成验证。
3. 不引入跨服务直连数据库、LLM 交易写入、Redis 库存事实源或 Kafka 缓存删除等架构漂移。
4. 已检查 diff 不包含无关改动和敏感信息；Git 初始化后，为该里程碑创建独立、可验证的 commit。
