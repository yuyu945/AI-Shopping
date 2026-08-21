# 智选购 AI Shopping

M1.1 已完成：Go-zero Gateway 与五个服务启动骨架、共享运行时基础、MySQL/Redis/Kafka/Milvus/MinIO 本地依赖和五个逻辑 schema 已建立。

## Local bootstrap

1. 复制 `.env.example` 为本地 `.env`，填写 `MYSQL_ROOT_PASSWORD`、`MYSQL_PASSWORD`、`MINIO_ACCESS_KEY` 和 `MINIO_SECRET_KEY`。真实值只保存在本地环境，不提交到 Git。
2. 启动依赖：`docker compose -f deploy/docker-compose.yml up -d`。
3. 验证 schema：设置 `AI_SHOPPING_MYSQL_DSN` 后运行 `pwsh -File scripts/verify_schema.ps1`。product-service 启动时将该 DSN 的 database 固定为 `catalog_db`，避免误连其他逻辑 schema。
4. 验证代码：`go test ./...`、`go vet ./...`。

Compose 宿主端口默认只绑定 `127.0.0.1`。如果本机 `6379` 已被占用，可用 `REDIS_PORT=6380` 启动，并将应用连接地址同步为 `localhost:6380`；容器网络内仍使用 `redis:6379`。

## M1.2 商品与用户读链路

商品 seed 是幂等的。设置本地 `MYSQL_ROOT_PASSWORD` 后运行：

```powershell
pwsh -File scripts/seed_catalog.ps1
```

product-service 读取 MySQL 商品事实并以 Cache Aside 方式缓存详情；Redis 不可用时会自动回源 MySQL。Gateway 商品接口：`GET /api/v1/products`、`GET /api/v1/products/{id}`，支持 `keyword`、`category_id`、`page`、`page_size` 和可选 `sku_id`。

默认 product-service 配置使用 etcd 服务发现；宿主机直连调试需确保 etcd 暴露 `127.0.0.1:2379`，或使用不含 `Etcd` 段的临时 RPC 配置。

M1.2 的商品读链路已完成真实 Docker MySQL/Redis 依赖和 Gateway HTTP 验证。用户认证链路也已完成：Gateway 公开 `POST /api/v1/auth/register`、`POST /api/v1/auth/login`，并通过 JWT 保护 `/api/v1/users/me`、`/api/v1/users/me/profile` 和 `/api/v1/users/me/addresses`。`AI_SHOPPING_JWT_SECRET` 是 Gateway 和 user-service 的必填环境变量，必须至少 32 bytes，且只保存在本地环境或秘密管理系统。

真实 Docker MySQL 验证覆盖两位用户的注册与登录、JWT Profile 访问、首地址默认、切换默认地址、地址列表及跨用户地址删除返回 404。JWT、密码、密码 Hash、地址和电话不会出现在 HTTP 响应或服务日志中。

M1.2 的 scheduler 延迟二次缓存删除及失败重试尚未实现；后续交易、RAG 和 Agent 功能按 `docs/TASKS.md` 的里程碑实现。
