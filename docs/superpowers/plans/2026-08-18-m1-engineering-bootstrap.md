# M1 Engineering Bootstrap Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Create a locally runnable Go-zero microservice foundation for 智选购 M1.1, with safe configuration, dependency containers, and ownership-aligned database schemas.

**Architecture:** A single Go workspace contains a public Go-zero API gateway and five independently executable gRPC services. Shared code is limited to configuration, error, logging, and trace propagation primitives; every service keeps its own configuration and future schema ownership. Docker Compose starts only stateful dependencies, while application binaries run locally during the first milestone.

**Tech Stack:** Go 1.25, go-zero, MySQL 8, Redis 7, Kafka (KRaft), Milvus standalone, MinIO, Docker Compose, `go test`, `go vet`.

---

### Task 1: Create the repository and Go workspace baseline

**Files:**
- Create: `.gitignore`
- Create: `go.work`
- Create: `go.mod`
- Create: `Makefile`
- Create: `.env.example`
- Modify: `docs/TASKS.md`

- [ ] **Step 1: Add the baseline files and mark M1.1 as in progress**

Use module path `github.com/yuyu945/AI-Shopping`; ignore `.env`, generated local data, binaries, coverage output, editor files, and local Docker volumes. `go.work` must contain `use .`; `.env.example` must define variable names only, with non-sensitive local defaults where possible.

- [ ] **Step 2: Validate workspace and environment-file safety**

Run: `go work sync; go list ./...; git check-ignore .env`

Expected: Go discovers the root module and Git ignores `.env`.

- [ ] **Step 3: Commit the baseline**

Run: `git add .gitignore go.work go.mod Makefile .env.example docs/TASKS.md && git commit -m "chore: bootstrap Go workspace"`

Expected: one focused commit with no credentials.

### Task 2: Build shared configuration, errors, logs, and trace primitives with tests

**Files:**
- Create: `internal/platform/config/config.go`
- Create: `internal/platform/config/config_test.go`
- Create: `internal/platform/apperror/error.go`
- Create: `internal/platform/apperror/error_test.go`
- Create: `internal/platform/trace/trace.go`
- Create: `internal/platform/trace/trace_test.go`
- Create: `internal/platform/logging/logger.go`

- [ ] **Step 1: Write failing tests for required environment values and trace propagation**

```go
func TestLoadRequiresDatabaseDSN(t *testing.T) {
    t.Setenv("AI_SHOPPING_MYSQL_DSN", "")
    _, err := Load()
    if err == nil || !strings.Contains(err.Error(), "AI_SHOPPING_MYSQL_DSN") {
        t.Fatalf("Load() error = %v, want missing database variable", err)
    }
}

func TestEnsureTraceIDPreservesExistingValue(t *testing.T) {
    ctx := context.WithValue(context.Background(), traceKey{}, "trace-existing")
    if got := EnsureTraceID(ctx); got != "trace-existing" {
        t.Fatalf("EnsureTraceID() = %q", got)
    }
}
```

- [ ] **Step 2: Run the focused tests and confirm the expected missing-symbol failure**

Run: `go test ./internal/platform/config ./internal/platform/trace`

Expected: FAIL because `Load`, `traceKey`, and `EnsureTraceID` do not exist yet.

- [ ] **Step 3: Implement the minimal primitives**

`config.Load` reads only `AI_SHOPPING_*` variables and returns a named error for each missing mandatory dependency DSN/endpoint. `apperror` exposes stable HTTP-safe codes (`INVALID_ARGUMENT`, `UNAUTHENTICATED`, `NOT_FOUND`, `OUT_OF_STOCK`, `IDEMPOTENCY_CONFLICT`, `DEPENDENCY_TIMEOUT`, `INTERNAL`); `trace.EnsureTraceID` returns a caller-provided trace ID or creates one; the logger writes JSON without configuration values.

- [ ] **Step 4: Verify focused and package-wide tests**

Run: `go test ./...; go vet ./...`

Expected: PASS with no vet findings.

- [ ] **Step 5: Commit the shared platform layer**

Run: `git add internal/platform && git commit -m "feat: add service platform primitives"`

Expected: one commit containing tests and the minimal implementation.

### Task 3: Add Gateway and service executable skeletons

**Files:**
- Create: `apps/gateway/main.go`
- Create: `apps/gateway/etc/gateway.yaml`
- Create: `apps/gateway/internal/handler/health_handler.go`
- Create: `apps/gateway/internal/handler/health_handler_test.go`
- Create: `services/user-service/cmd/main.go`
- Create: `services/user-service/etc/user-service.yaml`
- Create: `services/product-service/cmd/main.go`
- Create: `services/product-service/etc/product-service.yaml`
- Create: `services/order-service/cmd/main.go`
- Create: `services/order-service/etc/order-service.yaml`
- Create: `services/knowledge-service/cmd/main.go`
- Create: `services/knowledge-service/etc/knowledge-service.yaml`
- Create: `services/agent-service/cmd/main.go`
- Create: `services/agent-service/etc/agent-service.yaml`

- [ ] **Step 1: Write the gateway health-handler test**

```go
func TestHealthHandlerReturnsTraceID(t *testing.T) {
    req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
    req.Header.Set("X-Trace-ID", "trace-test")
    recorder := httptest.NewRecorder()

    HealthHandler().ServeHTTP(recorder, req)

    if recorder.Code != http.StatusOK {
        t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
    }
    if recorder.Header().Get("X-Trace-ID") != "trace-test" {
        t.Fatalf("X-Trace-ID = %q", recorder.Header().Get("X-Trace-ID"))
    }
}
```

- [ ] **Step 2: Run the test and confirm it fails because the handler is missing**

Run: `go test ./apps/gateway/internal/handler`

Expected: FAIL with `undefined: HealthHandler`.

- [ ] **Step 3: Implement the health endpoint and service process contracts**

The gateway must expose `GET /healthz`, return `{"status":"ok"}`, and return or generate `X-Trace-ID`. Each service executable must load its own YAML file, validate shared environment configuration, expose a named service identity, and exit with a stable actionable error when required configuration is missing. No service may contain business DAO, SQL, Agent, or Kafka consumer code in this task.

- [ ] **Step 4: Verify compilation and health behavior**

Run: `go test ./...; go vet ./...; go run ./apps/gateway -f apps/gateway/etc/gateway.yaml`

Expected: tests and vet pass; `curl http://127.0.0.1:8888/healthz` returns HTTP 200 and `X-Trace-ID`.

- [ ] **Step 5: Commit executable skeletons**

Run: `git add apps services && git commit -m "feat: add gateway and service skeletons"`

Expected: one commit containing only service startup and health-check code.

### Task 4: Add stateful dependency Compose stack and schema migrations

**Files:**
- Create: `deploy/docker-compose.yml`
- Create: `deploy/mysql/init/00-create-schemas.sql`
- Create: `deploy/mysql/init/01-user-schema.sql`
- Create: `deploy/mysql/init/02-catalog-schema.sql`
- Create: `deploy/mysql/init/03-trade-schema.sql`
- Create: `deploy/mysql/init/04-agent-schema.sql`
- Create: `deploy/mysql/init/05-knowledge-schema.sql`
- Create: `deploy/kafka/create-topics.sh`
- Create: `deploy/README.md`

- [ ] **Step 1: Add schema acceptance tests before schema SQL**

Create `scripts/verify_schema.ps1` that waits for MySQL, runs `SHOW DATABASES`, and fails unless the five required schemas are present. Initially it must fail because the script and schema stack are absent.

- [ ] **Step 2: Confirm the expected failure**

Run: `pwsh ./scripts/verify_schema.ps1`

Expected: FAIL because `scripts/verify_schema.ps1` does not exist.

- [ ] **Step 3: Implement Compose, migrations, and dependency health checks**

Compose must start MySQL, Redis, Kafka in KRaft mode, Milvus standalone with its required etcd and MinIO objects, and MinIO. Containers must have health checks where the image supports them. SQL must create exactly `user_db`, `catalog_db`, `trade_db`, `agent_db`, and `knowledge_db`, with no cross-schema foreign keys. Create only the M1 user/catalog tables needed for later read-path work; trade, agent, and knowledge migration files establish their owned schemas without prematurely adding full M2-M4 tables. The Kafka topic script must create the four documented topics with stable names.

- [ ] **Step 4: Verify Docker configuration and schema initialization**

Run: `docker compose -f deploy/docker-compose.yml config; docker compose -f deploy/docker-compose.yml up -d; pwsh ./scripts/verify_schema.ps1`

Expected: Compose configuration is valid, declared dependencies are healthy, and exactly the five logical schemas exist.

- [ ] **Step 5: Commit local infrastructure**

Run: `git add deploy scripts && git commit -m "feat: add local dependency stack and schemas"`

Expected: one commit containing only local deployment and migration assets.

### Task 5: Run M1.1 verification and close the milestone

**Files:**
- Modify: `docs/TASKS.md`
- Modify: `docs/PLAN.md`
- Modify: `README.md`

- [ ] **Step 1: Document reproducible startup and validation commands**

`README.md` must state the required `.env` variables, the Compose start command, Gateway health command, and the distinction between dependency readiness and application-chain readiness.

- [ ] **Step 2: Run final verification**

Run: `gofmt -w apps internal services; go test ./...; go vet ./...; docker compose -f deploy/docker-compose.yml config; git diff --check; git status --short`

Expected: formatting produces no unintended diff, tests/vet/config checks pass, and only documented milestone changes remain.

- [ ] **Step 3: Update the milestone status and commit**

Run: `git add README.md docs/TASKS.md docs/PLAN.md && git commit -m "docs: record M1.1 bootstrap validation"`

Expected: the repository history consists of focused, verified M1.1 commits. Do not push without an explicit user request.
