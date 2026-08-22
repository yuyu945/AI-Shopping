# 智选购 AI Shopping

M1 已完成：Go-zero Gateway 与五个服务启动骨架、共享运行时基础、MySQL/Redis/Kafka/Milvus/MinIO 本地依赖、五个逻辑 schema，以及用户认证、商品读取和商品缓存失效链路均已建立。

## Local bootstrap

1. 复制 `.env.example` 为本地 `.env`，填写 `MYSQL_ROOT_PASSWORD`、`MYSQL_PASSWORD`、`MINIO_ACCESS_KEY` 和 `MINIO_SECRET_KEY`。真实值只保存在本地环境，不提交到 Git。
2. 启动依赖：`docker compose -f deploy/docker-compose.yml up -d`。
3. 验证 schema：设置 `AI_SHOPPING_MYSQL_DSN` 后运行 `pwsh -File scripts/verify_schema.ps1`。product-service 启动时将该 DSN 的 database 固定为 `catalog_db`，避免误连其他逻辑 schema。
4. 验证代码：`go test ./...`、`go vet ./...`。

## Trade schema upgrade

已有 M1 MySQL volume 不会重新执行 `deploy/mysql/init`。设置 `AI_SHOPPING_MYSQL_DSN` 后，运行 `pwsh -File scripts/apply_migrations.ps1` 升级 `trade_db`。migration runner 只从该环境变量读取 DSN metadata（app user、host、port），不接受命令行 DSN；MySQL client authentication 使用 Compose container 内的 `MYSQL_PASSWORD`，二者均不会被记录或输出。脚本按文件名顺序记录 `trade_db.schema_migrations`；已应用版本会安全跳过。M2.1 会创建购物车和订单快照表，并在 `cart_items.quantity` 与 `order_items.quantity` 上使用 MySQL 8.4 enforced `CHECK (quantity > 0)`。

可在 disposable Compose 环境验证 M1 到 M2.1 升级。设置本地临时 `MYSQL_PASSWORD`、`MYSQL_ROOT_PASSWORD` 后运行：`$env:AI_SHOPPING_TRADE_MIGRATION_INTEGRATION='1'; pwsh -File scripts/test_trade_migration_integration.ps1 -MySQLPort 33306`。脚本使用 UUID project、清空仅 M1 的 `trade_db` 状态，连续升级两次并验证版本、表、FK、金额列和 CHECK；未设置 opt-in guard 时会显式跳过且不写入。

## M2.1 购物车与订单快照集成验证

订单快照 integration harness 只启动专用 MySQL Compose project，并在 repository/service 层验证购物车增改删、非本人地址拒绝、重复 `request_id` 重放，以及商品标题和价格变更后订单快照保持不变。它不启动 Gateway 或三服务 gRPC，因此不能作为 HTTP 端到端验证的替代。

设置临时本地 `MYSQL_PASSWORD` 和 `MYSQL_ROOT_PASSWORD` 后显式运行：

```powershell
$env:AI_SHOPPING_ORDER_SNAPSHOT_INTEGRATION = '1'
pwsh -File scripts/test_order_snapshot_integration.ps1 -MySQLPort 3310
```

未设置 `AI_SHOPPING_ORDER_SNAPSHOT_INTEGRATION=1` 时，harness 输出 `SKIP` 且不连接 Docker 或 MySQL。实际运行使用固定项目 `m21orderverify`、随机 UUID `run_id` 和 `trade_db.order_snapshot_integration_guards`；测试只接受 `trade_db` DSN、校验 guard 后才写入。脚本不会通过命令行传递凭证，运行结束会精确删除 Compose 容器、volume、network 及测试 fixture，并恢复其修改的 process environment。

Compose 宿主端口默认只绑定 `127.0.0.1`。如果本机 `6379` 已被占用，可用 `REDIS_PORT=6380` 启动，并将应用连接地址同步为 `localhost:6380`；容器网络内仍使用 `redis:6379`。

## M1.2 商品与用户读链路

商品 seed 是幂等的。设置本地 `MYSQL_ROOT_PASSWORD` 后运行：

```powershell
pwsh -File scripts/seed_catalog.ps1
```

product-service 读取 MySQL 商品事实并以 Cache Aside 方式缓存详情；Redis 不可用时会自动回源 MySQL。商品详情写入 transaction 同时持久化 `cache_invalidation_tasks`，提交后立即删除全商品和各 SKU cache key；立即删除失败不会回滚已经提交的 MySQL 事实，scheduler/worker 会执行延迟二次删除并对失败任务退避重试。Redis 只承担读优化，不是商品、库存或失效任务的事实源。Gateway 商品接口：`GET /api/v1/products`、`GET /api/v1/products/{id}`，支持 `keyword`、`category_id`、`page`、`page_size` 和可选 `sku_id`。

默认 product-service 配置使用 etcd 服务发现；宿主机直连调试需确保 etcd 暴露 `127.0.0.1:2379`，或使用不含 `Etcd` 段的临时 RPC 配置。

M1.2 的商品读链路已完成真实 Docker MySQL/Redis 依赖和 Gateway HTTP 验证。用户认证链路也已完成：Gateway 公开 `POST /api/v1/auth/register`、`POST /api/v1/auth/login`，并通过 JWT 保护 `/api/v1/users/me`、`/api/v1/users/me/profile` 和 `/api/v1/users/me/addresses`。`AI_SHOPPING_JWT_SECRET` 是 Gateway 和 user-service 的必填环境变量，必须至少 32 bytes，且只保存在本地环境或秘密管理系统。

真实 Docker MySQL 验证覆盖两位用户的注册与登录、JWT Profile 访问、首地址默认、切换默认地址、地址列表及跨用户地址删除返回 404。JWT、密码、密码 Hash、地址和电话不会出现在 HTTP 响应或服务日志中。

缓存失效 integration test 使用真实 MySQL/Redis，默认不连接外部依赖。`AI_SHOPPING_MYSQL_DSN` 必须指向专用 Docker project 的 `catalog_db`，`AI_SHOPPING_REDIS_ADDR` 必须指向同一专用环境；测试会在写入前确认 `cache_invalidation_tasks` 和所选 Redis DB 都为空。再显式运行：

```powershell
$runID = [guid]::NewGuid().ToString()
$env:MYSQL_ROOT_PASSWORD = '<temporary local value>'
pwsh -File scripts/prepare_cache_invalidation_integration.ps1 -RunID $runID

$env:AI_SHOPPING_INTEGRATION = '1'
$env:AI_SHOPPING_INTEGRATION_ISOLATED = 'm12cacheverify'
$env:AI_SHOPPING_REDIS_DB = '15'
$env:AI_SHOPPING_INTEGRATION_RUN_ID = $runID
go test -tags=integration ./services/product-service/internal/catalog -run '^TestCacheInvalidationIntegration$' -count=1 -v
```

先以 `COMPOSE_PROJECT_NAME=m12cacheverify` 启动专用 MySQL/Redis project；准备脚本只接受健康的 `m12cacheverify-mysql-1` 容器，并在该 disposable `catalog_db` 写入 UUID guard row。`AI_SHOPPING_INTEGRATION_ISOLATED` 必须精确为 `m12cacheverify`，`AI_SHOPPING_REDIS_DB` 必须为非零的专用 Redis DB index，`AI_SHOPPING_INTEGRATION_RUN_ID` 必须匹配数据库中的 guard row；任一隔离条件不满足时测试不会执行写入。测试会动态发现 seed 商品、预载全商品与 SKU cache key，并验证正常立即删除、持久化延迟任务、worker 完成，以及首次 Redis 删除失败后由 `PENDING` 收敛到 `DONE`；测试结束只删除实际创建的 task ID，并仅在产品仍处于测试写入的版本时恢复 fixture。M1.2 已完成，下一里程碑是 M2 交易闭环与一致性；RAG 和 Agent 功能随后按 `docs/TASKS.md` 实现。
