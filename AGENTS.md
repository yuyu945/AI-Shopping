# 智选购 MVP 开发规则

## 1. 项目目标

智选购是一个基于 Go 的 AI 电商导购 MVP。必须交付并验证以下主链路：

```text
自然语言购买需求
-> Agent 调用受控 Tool
-> 后端校验价格 / 库存 / 优惠
-> 保存可回放推荐结果
-> 购物车与余额支付
-> 评价和行为事件异步处理
```

完整设计以 [docs/superpowers/specs/2026-08-18-ai-shopping-guide-mvp-design.md](docs/superpowers/specs/2026-08-18-ai-shopping-guide-mvp-design.md) 为准。实现与设计冲突时，先更新设计并说明取舍，再修改代码。

本项目仍处于 MVP 阶段。不要擅自加入完整营销后台、多 Agent、秒杀库存预扣、真实第三方支付、模型计费或与主链路无关的功能。

## 2. 工作方式

1. 修改前先读取相关服务、接口、数据模型、测试和设计文档，确认调用方与边界。
2. 优先做一条可运行的完整链路，不要只堆积孤立模块。
3. 需求、数据结构、状态机或消息语义变化时，同步更新设计文档、接口定义和测试。
4. 以最小改动完成任务；不做无关重构，不引入未被验证的新框架。
5. 任何完成声明必须附带实际执行过的验证结果。不能把“服务能启动”描述为“主链路可用”。

### 2.1 执行模式分级

默认使用快速模式。只有任务风险升高、用户明确要求，或代码审阅发现交易/安全/迁移/并发风险时，才升级到标准或严格模式。不要把严格模式作为日常默认流程。

| 模式 | 适用场景 | 执行方式 | 验证要求 |
| --- | --- | --- | --- |
| 快速模式 | 文档、小 bug、单文件或少量文件改动、低风险测试补齐 | 主智能体直接调查、实现、自审、提交；不默认创建子智能体 | 运行受影响包的 `go test` / `go vet` / `gofmt`；必要时再跑全量 |
| 标准模式 | 普通 feature、跨 2-5 个模块、接口或数据结构有小幅变化 | 主智能体实现；最多启用一个 reviewer 子智能体审关键 diff | 先跑 targeted tests；阶段收尾跑 `go test ./...`、`go vet ./...`、`git diff --check` |
| 严格模式 | 支付、库存、认证、权限、迁移、Kafka 幂等、并发、数据一致性、跨服务恢复 | 可使用实现子智能体 + spec review + quality review；重要里程碑再做 final review | 必须跑 targeted tests、全量 tests、vet、diff check；能安全运行的 guard/integration 脚本也要跑 |

模式选择规则：

- 用户说“尽快”“不要过度设计”“收尾”时，除非命中高风险触发条件，否则保持快速或标准模式。
- 子智能体只在用户明确要求、任务可并行拆分，或严格模式确有收益时使用；不要为了流程完整而派发子智能体。
- 代码审阅按风险启用：快速模式用主智能体自审；标准模式最多一个 reviewer；严格模式才使用双阶段审阅。
- 验证按风险分层：小改动先跑受影响包；阶段收口、合并、push 前再跑全量验证。
- 如果快速模式中发现交易、安全、迁移、并发或数据一致性风险，立即升级到严格模式，并说明升级原因。

## 3. 服务边界

| 服务 | 拥有能力 | 禁止事项 |
| --- | --- | --- |
| `user-service` | 注册登录、JWT、用户画像、地址、余额查询 | 查询或修改其他用户数据 |
| `product-service` | 商品、SKU、库存、优惠、商品搜索 | 把 Redis 当库存事实源 |
| `order-service` | 购物车、订单、余额支付、评价 | 跨服务直接写商品或用户表 |
| `knowledge-service` | MinIO 文档、版本、Chunk、Milvus 检索 | 同步阻塞等待解析与 Embedding 完成 |
| `agent-service` | 会话、Run/Step、Tool 编排、推荐快照、SSE | 直接改库存、余额、订单或执行 SQL |

- 使用 Go-zero API Gateway 对外提供 HTTP 接口，服务间使用 gRPC。
- 每个服务只能拥有并直接访问自己的 schema/table；跨服务只传 ID、不可变快照或 gRPC DTO，禁止共享 DAO 和跨服务直连数据库。
- MVP 可以部署在一个 MySQL 实例，但逻辑 schema 固定为 `user_db`、`catalog_db`、`trade_db`、`agent_db`、`knowledge_db`。
- 物理外键只允许在同一服务 schema 内使用；跨服务关系由业务校验和不可变快照维护。

## 4. Agent 与 LLM 规则

- 只在 `agent-service` 中使用 Eino 和模型 Provider；业务服务不得依赖模型 SDK。
- Tool 必须是 Typed Tool，并声明输入 Schema、输出 DTO、权限、超时和最大返回数量。
- 模型永远不能直接执行 SQL、访问 Redis、写订单、扣库存、扣余额或修改优惠。
- `get_user_profile` 等用户数据 Tool 必须由服务端从 JWT 上下文注入当前用户 ID，不能信任模型提供的用户 ID。
- 单次 `AgentRun` 最多 8 个 Step，总超时 30 秒。每个 Step 都必须有明确的 `RUNNING`、`SUCCEEDED`、`FAILED` 或 `TIMEOUT` 结果。
- `AgentRun` 是任务主记录；`AgentStep` 是轮次和 Tool 调用记录。二者都必须带关联 ID、耗时、结构化错误和脱敏后的输入输出。
- 模型只能提交推荐 SKU ID、排序和理由。价格、库存、优惠、可售状态必须由后端 Tool 重新查询并写入 `recommendations` 快照。
- 模型或 Tool 原始报错不能直出给用户。使用稳定的错误分类和可行动的简短文案。

## 5. RAG 与知识库规则

- 原始资料存 MinIO，MySQL 保存文档元数据、版本、状态、错误和 Chunk 元数据，Milvus 保存向量及筛选元数据。
- 文档处理固定为：上传 -> `knowledge.document.ingest` -> 解析/切分 -> `knowledge.chunk.embed` -> Embedding -> Milvus -> `READY`。
- `DETAIL`、`SPEC`、`FAQ`、`AFTER_SALE` 是固定资料类型；必须按 `product_id`、`doc_type`、`version` 过滤，只检索当前 `READY` 版本。
- Chunk 必须保存来源页码/章节、内容 Hash、文档版本和 `chunk_id`。同一 `document_id + chunk_index` 只能入库一次。
- Kafka 消息以业务 `event_id` 幂等。消费者失败进入 retry topic，达到阈值进入 dead-letter topic；重放不会产生重复 Chunk 或重复统计。
- RAG 不可用时只可降级为商品结构化字段或规则问答，不得编造资料中不存在的信息。

## 6. 交易、一致性与缓存规则

- MySQL 是订单、金额、库存、优惠资格和推荐快照的最终事实源；金额使用 `DECIMAL(12,2)`，禁止 float。
- 商品使用 SPU/SKU 两级模型。价格和规格位于 SKU；可售库存位于 `inventory`，不要存到商品 SPU 或 Redis 作为事实字段。
- 库存扣减必须用单条条件更新，并按受影响行数判断结果：

```sql
UPDATE inventory
SET available_qty = available_qty - ?, version = version + 1
WHERE sku_id = ? AND available_qty >= ?;
```

- Redis 只缓存商品详情、会话和短期状态。下单、支付、库存扣减不得以缓存结果作为最终判断。
- 商品写入事务提交后立即删除缓存，并创建持久化的 `cache_invalidation_tasks`。延迟二次删除只用于缩小旧值回填窗口，失败必须重试；不要在请求 Goroutine 中 `sleep`。
- 创建订单以 `(user_id, request_id)` 幂等；余额流水以 `(biz_type, biz_id, direction)` 唯一约束幂等。重复请求返回第一次已成功的结果。
- 余额支付 transaction 内必须完成余额校验、库存条件扣减、订单状态变更、钱包流水和 Outbox 写入。外部消息投递不参与该 transaction。
- 订单和推荐结果必须保存商品、价格、规格、优惠、地址等必要快照，历史记录不得依赖当前商品数据回填。

## 7. Kafka 与异步规则

- Kafka 用于知识库处理、订单/评价/行为事件和运营分析，不用作缓存删除或通用延迟队列。
- 必须先在本地 transaction 写入 `outbox_events`，再由 Worker 投递 Kafka；禁止“先提交 DB 再尝试发 Kafka”而没有补偿记录。
- Topic key 必须反映顺序边界：知识库使用 `document_id`，行为使用 `user_id`，评价使用 `product_id`。
- Consumer 必须实现 `event_consumptions(event_id, consumer_group)` 幂等；不能依赖“Kafka 大概率不会重复”。
- Kafka 原生不提供精确延迟。重试使用 retry topic、`next_retry_at` 和退避策略；严格定时任务由 Go scheduler 发布事件。

## 8. 数据、隐私与安全规则

- 密码只保存强 Hash；JWT、模型 API Key、数据库密码和 MinIO 密钥只来自环境变量或秘密管理，不写入代码、日志、样例或文档。
- Agent 输入输出、地址、手机号、支付信息写日志或 Step 前必须脱敏。Agent 消息与 Step 原始内容默认只保留 90 天。
- 所有资源接口都进行资源归属校验。仅携带合法 ID 不代表有访问权限。
- API 错误使用统一错误码，区分参数错误、未授权、资源不存在、库存不足、幂等冲突、依赖超时和内部错误。
- 参数化 SQL 是唯一允许的 SQL 执行方式；模型文本、筛选条件和排序字段不得直接拼接到 SQL。

## 9. Go 编码规则

- 使用 `context.Context` 传递取消、超时、trace ID 和认证信息；禁止用全局变量传递请求态数据。
- 所有外部调用（gRPC、Kafka、Milvus、MinIO、LLM、Redis）必须设置超时，错误要携带操作和依赖名称。
- 只在相互独立的 I/O 任务中并发；使用 `errgroup` 或有界 worker pool，并确保取消后不泄漏 Goroutine。
- DTO、领域对象和持久化对象分离；不要让数据库模型直接作为 HTTP/gRPC 响应。
- 导出的符号要有文档注释；复杂一致性、状态迁移和补偿逻辑必须解释不变量，而不是复述代码。
- 每次修改至少运行 `gofmt`；有 Go 模块后运行受影响包的 `go test` 和 `go vet`。

## 10. 测试与验收规则

- 新的业务分支至少有 table-driven unit tests，覆盖正常、边界和失败路径。
- 订单测试必须覆盖：库存不足、余额不足、重复 `request_id`、重复支付和 transaction 回滚。
- Agent 测试必须覆盖：无效 Tool 参数、Tool 超时、超过最大 Step、模型返回无效 SKU、后端二次校验失败。
- RAG 测试必须覆盖：文档重复投递、版本切换、Embedding 失败重试和旧版本不被召回。
- Kafka 测试必须覆盖：重复消费、retry、dead-letter 和重放后的幂等结果。
- 缓存测试必须覆盖：缓存未命中、写后立即删除失败、延迟二次删除和库存不依赖缓存。
- 性能指标必须记录压测机器、并发、请求数、数据规模、缓存预热状态和工具版本；没有可重复测试结果，禁止在简历或文档中写性能数字。

## 11. 文档与提交规则

- API、数据库、Topic、状态机或服务边界变化时，同步更新 MVP 设计文档。
- 新增环境变量时更新 `.env.example`，但绝不提交真实值。
- 每个可验证里程碑使用独立、聚焦的 commit；提交前检查 diff、运行受影响测试和格式化。
- 不推送、不创建远程仓库、不重写历史，除非用户明确要求。
- 当前工作区尚未初始化 Git。初始化后再执行 commit 规则；在此之前记录验证命令和结果即可。
