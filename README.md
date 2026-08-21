# 智选购 AI Shopping

M1.1 已完成：Go-zero Gateway 与五个服务启动骨架、共享运行时基础、MySQL/Redis/Kafka/Milvus/MinIO 本地依赖和五个逻辑 schema 已建立。

## Local bootstrap

1. 复制 `.env.example` 为本地 `.env`，填写 `MYSQL_ROOT_PASSWORD`、`MYSQL_PASSWORD`、`MINIO_ACCESS_KEY` 和 `MINIO_SECRET_KEY`。真实值只保存在本地环境，不提交到 Git。
2. 启动依赖：`docker compose -f deploy/docker-compose.yml up -d`。
3. 验证 schema：设置 `AI_SHOPPING_MYSQL_DSN` 后运行 `pwsh -File scripts/verify_schema.ps1`。product-service 启动时将该 DSN 的 database 固定为 `catalog_db`，避免误连其他逻辑 schema。
4. 验证代码：`go test ./...`、`go vet ./...`。

Compose 宿主端口默认只绑定 `127.0.0.1`。如果本机 `6379` 已被占用，可用 `REDIS_PORT=6380` 启动，并将应用连接地址同步为 `localhost:6380`；容器网络内仍使用 `redis:6379`。

## M1.2 商品读链路

商品 seed 是幂等的。设置本地 `MYSQL_ROOT_PASSWORD` 后运行：

```powershell
pwsh -File scripts/seed_catalog.ps1
```

product-service 读取 MySQL 商品事实并以 Cache Aside 方式缓存详情；Redis 不可用时会自动回源 MySQL。Gateway 商品接口：`GET /api/v1/products`、`GET /api/v1/products/{id}`，支持 `keyword`、`category_id`、`page`、`page_size` 和可选 `sku_id`。

默认 product-service 配置使用 etcd 服务发现；宿主机直连调试需确保 etcd 暴露 `127.0.0.1:2379`，或使用不含 `Etcd` 段的临时 RPC 配置。

M1.2 已完成真实 Docker MySQL/Redis 依赖和 Gateway HTTP 读链路验证。后续交易、RAG 和 Agent 功能按 `docs/TASKS.md` 的里程碑实现。
