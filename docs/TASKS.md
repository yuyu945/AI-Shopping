# 智选购 MVP 实施任务清单

## 1. 使用方式

- 当前阶段：Git 已初始化，M1.1 工程 bootstrap 进行中；其余任务尚未开始。
- 任务按依赖顺序执行；每完成一个里程碑，先运行其验证命令、检查文档同步，再创建一个聚焦 commit。
- 所有实现必须遵守 [AGENTS.md](../AGENTS.md)、[PRD.md](PRD.md)、[architecture.md](architecture.md)、[interaction.md](interaction.md) 和 [MVP 设计文档](智选购-ai导购-mvp-design.md)。设计与实现不一致时，先更新设计并说明取舍。
- 状态标记：`[ ]` 未开始，`[-]` 进行中，`[x]` 已完成，`[!]` 受阻。M1.1 正在进行，其余任务未开始。

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

### M1.1 初始化工程与本地依赖（进行中）

- [ ] 创建 Go workspace、Go-zero API Gateway 与 `user-service`、`product-service`、`order-service`、`knowledge-service`、`agent-service` 的最小 gRPC 服务骨架。
- [ ] 建立统一配置加载、环境变量校验、请求 `trace_id` 透传、HTTP/gRPC 错误码和结构化日志基础设施。
- [ ] 编写 Docker Compose，启动 MySQL、Redis、Kafka、Milvus、MinIO 与开发所需 Worker；真实凭证只来自本地 `.env`，`.env.example` 仅列变量名和非敏感示例。
- [ ] 创建五个 MySQL schema 与迁移机制：`user_db`、`catalog_db`、`trade_db`、`agent_db`、`knowledge_db`。

验收：空环境执行启动命令后，依赖健康检查成功；缺少必填环境变量时服务以明确错误退出，不输出敏感配置。

### M1.2 用户、商品与缓存读路径

- [ ] 实现注册登录、JWT、中间件身份注入、用户画像和地址的归属校验。
- [ ] 实现分类、品牌、SPU、SKU、图片、库存与优惠的 schema、迁移和演示种子数据。
- [ ] 实现商品列表、SKU 详情、关键词/分类筛选，以及 `search_products`、`get_price_stock`、`get_discount` gRPC DTO。
- [ ] 实现 Cache Aside 商品详情缓存：缓存未命中读 MySQL 后回填，商品写 transaction 提交后立即删除缓存并写入 `cache_invalidation_tasks`。
- [ ] 实现 scheduler 执行延迟二次删除与失败重试，禁止在 HTTP 请求中 sleep。

测试：商品筛选、SKU 切换、缓存未命中/命中、提交后删除失败、延迟删除重试；确认库存 API 与扣减逻辑不依赖 Redis。

## 4. M2：交易闭环与一致性

### M2.1 购物车与订单快照

- [ ] 创建购物车、购物车项、订单、订单项、钱包账户、钱包流水和 Outbox 迁移，金额字段统一为 `DECIMAL(12,2)`。
- [ ] 实现购物车增改删和用户隔离。
- [ ] 实现创建订单 API：校验地址和购物车，按 `(user_id, request_id)` 生成幂等的 `PENDING_PAYMENT` 订单，写商品、规格、价格、优惠和地址快照。
- [ ] 对订单列表与详情只使用订单快照展示，不回填当前商品字段。

测试：空购物车、非本人地址、重复 `request_id`、商品下架、订单快照在商品变更后仍保持不变。

### M2.2 余额支付本地事务

- [ ] 实现余额支付 transaction：锁定订单与钱包、校验状态和余额、按 SKU 执行库存条件更新、更新订单、写钱包流水和 Outbox。
- [ ] 按受影响行数判断库存扣减结果；库存不足、余额不足、非法订单状态或任一步骤异常必须回滚 transaction。
- [ ] 以钱包流水唯一约束和已支付订单判断保障重复支付返回首次结果，不重复扣款或扣库存。
- [ ] 实现 Outbox Worker 扫描、发布和失败退避，保留未成功投递记录。

测试：库存不足、余额不足、重复支付、订单状态冲突、transaction 回滚、Outbox 首次发布失败后补偿成功。验收时需查询余额、库存、订单和流水，确认不存在部分成功。

## 5. M3：异步知识库与 RAG

### M3.1 文档上传与事件可靠投递

- [ ] 实现资料上传至 MinIO 和 `knowledge_documents` 记录，支持 DETAIL、SPEC、FAQ、AFTER_SALE 类型。
- [ ] 上传 transaction 内写入 `knowledge.document.ingest` Outbox 事件，接口立即返回文档号、版本和 `PENDING` 状态。
- [ ] 建立 `outbox_events`、`event_consumptions`、`embedding_tasks` 迁移和通用事件信封，至少包含 `event_id`、事件类型、发生时间、聚合 ID 和 payload 版本。
- [ ] 配置知识库 retry/dead-letter topic 与退避字段；重放入口必须保留原始事件 ID 和失败原因。

测试：同一文档重复上传、同一事件重复投递、发布失败重试、超过阈值进入 dead-letter。

### M3.2 解析、向量化和版本检索

- [ ] 实现 `knowledge.document.ingest` consumer：解析文件、规范化文本、按固定策略切分，写入带章节/页码、内容 Hash、版本的 Chunk。
- [ ] 实现 `knowledge.chunk.embed` consumer：Embedding、Milvus upsert 和文档状态更新；用 `(document_id, chunk_index)` 与内容 Hash 约束防重复。
- [ ] 实现只有当前 `READY` 版本可见的切换逻辑；新版本失败时旧版本继续可检索。
- [ ] 实现商品知识检索 gRPC，返回内容片段、文档类型、版本、章节/页码和相似度排序所需字段。

测试：Chunk 重复消费、Embedding 失败后重试、旧版本过滤、资料无依据问答的受控降级。

## 6. M4：Agent 导购与推荐校验

### M4.1 Agent 运行模型与受控 Tool

- [ ] 创建会话、消息、`agent_runs`、`agent_steps` 和推荐快照迁移，固定 Run/Step 状态机、索引和 90 天脱敏内容保留规则。
- [ ] 封装 Eino ChatModel、Embedding 与 Tool 适配层，使模型 Provider 细节不泄漏到业务服务。
- [ ] 实现 `search_products`、`get_user_profile`、`get_price_stock`、`get_discount`、`search_product_knowledge` Typed Tool，并为每个 Tool 配置输入 Schema、超时、最大返回数量与授权来源。
- [ ] 限制每次 Run 最多 8 个 Step、总超时 30 秒；所有外部调用使用带取消与超时的 `context.Context`。

测试：无效 Tool 参数、伪造用户 ID、Tool 超时、超过最大 Step、模型供应商错误；每种失败均落为明确 Run/Step 状态并返回稳定错误码。

### M4.2 推荐二次校验与可回放记录

- [ ] 定义模型最终输出 Schema，仅接受 SKU ID、排序和理由。
- [ ] 在写 `recommendations` 前并发但有界地重查价格、库存、优惠和可售状态，过滤无效 SKU，生成不可变快照。
- [ ] 持久化脱敏的模型/Tool 入参出参、耗时、异常、模型版本、Prompt 版本和 `trace_id`。
- [ ] 实现查询 Run 详情 API，返回最终推荐、校验状态和按步骤排序的时间线数据。

测试：模型返回不存在 SKU、下架 SKU、库存不足 SKU、伪造价格字段、优惠资格变化；确认最终推荐只包含后端校验成功的快照。

## 7. M5：用户交互、运营排障与事件分析

### M5.1 用户端主体验

- [ ] 实现商品列表、商品详情、SKU 切换、购物车、订单确认、余额支付、订单详情和评价页面，状态遵循 [interaction.md](interaction.md)。
- [ ] 实现 AI 导购页：发送自然语言需求、建立 SSE 订阅、渲染 Run 进度、推荐卡片和受控失败提示。
- [ ] 实现商品知识问答及来源展示；RAG 不可用时呈现受控降级，不生成无来源断言。
- [ ] 实现支付提交中、库存不足、余额不足、网络超时和重复提交后的订单状态回查。

验收：从自然语言需求到推荐、商品详情、购物车、建单和余额支付形成可操作闭环；文本、错误与按钮状态不重叠，移动端关键路径可用。

### M5.2 运营端最小排障能力与事件消费

- [ ] 实现资料列表、上传、版本详情、处理状态、失败原因和重试操作。
- [ ] 实现 Agent Run 列表、状态筛选、Run 详情、Step 时间线、`trace_id` 展示与脱敏详情。
- [ ] 实现评价提交后的 `review.events` 与用户行为 `behavior.events`，以 Outbox 投递 Kafka。
- [ ] 实现统计 consumer 的幂等记录、retry/dead-letter 处理和最小只读事件概览。

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
