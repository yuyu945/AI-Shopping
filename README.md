# 智选购 AI Shopping

M1.1 已完成：Go-zero Gateway 与五个服务启动骨架、共享运行时基础、MySQL/Redis/Kafka/Milvus/MinIO 本地依赖和五个逻辑 schema 已建立。

## Local bootstrap

1. 复制 `.env.example` 为本地 `.env`，填写 `MYSQL_ROOT_PASSWORD`、`MYSQL_PASSWORD`、`MINIO_ACCESS_KEY` 和 `MINIO_SECRET_KEY`。真实值只保存在本地环境，不提交到 Git。
2. 启动依赖：`docker compose -f deploy/docker-compose.yml up -d`。
3. 验证 schema：设置 `AI_SHOPPING_MYSQL_DSN` 后运行 `pwsh -File scripts/verify_schema.ps1`。
4. 验证代码：`go test ./...`、`go vet ./...`。

Compose 宿主端口默认只绑定 `127.0.0.1`。如果本机 `6379` 已被占用，可用 `REDIS_PORT=6380` 启动，并将应用连接地址同步为 `localhost:6380`；容器网络内仍使用 `redis:6379`。

当前阶段只验证基础设施与启动契约；用户、商品读路径、交易、RAG 和 Agent 功能按 `docs/TASKS.md` 的后续里程碑实现。
