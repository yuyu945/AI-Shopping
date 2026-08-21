# Local Dependency Stack

This Compose stack provisions the local dependencies for the AI Shopping MVP:
MySQL 8, Redis 7, single-node Kafka KRaft, MinIO, etcd, and Milvus standalone.
It does not start the Go services.

## Required local secrets

Copy the repository `.env.example` to `.env`, then set non-empty local values for:

- `MYSQL_PASSWORD`
- `MYSQL_ROOT_PASSWORD`
- `MINIO_ACCESS_KEY`
- `MINIO_SECRET_KEY`

These are deliberately not committed. `MYSQL_ROOT_PASSWORD` is only for the
MySQL root account, while `MYSQL_PASSWORD` is only for the local `app` account.
Use distinct local values and never use a production secret in `.env`.

Set the runtime DSN separately before starting a service or schema verification:

```powershell
$env:AI_SHOPPING_MYSQL_DSN = 'app:<local-password>@tcp(127.0.0.1:3306)/user_db?parseTime=true'
```

The sample is a shape only; replace `<local-password>` in your shell and do not
commit the resulting value. `verify_schema.ps1` parses this DSN and uses its
credentials and connection target with the MySQL client inside the running MySQL
container; it never prints the DSN or password.

## Start and verify

```powershell
docker compose --env-file .env -f deploy/docker-compose.yml up -d
pwsh -NoProfile -File scripts/verify_schema.ps1
docker compose --env-file .env -f deploy/docker-compose.yml ps
```

The init SQL creates exactly these application schemas: `user_db`, `catalog_db`,
`trade_db`, `agent_db`, and `knowledge_db`. Only the M1 user and catalog read
tables are present. Foreign keys are confined to their owning schema.
The local `app` account has explicit privileges on all five schemas so the MVP
can use one application credential. Future deployments should replace it with
least-privilege service-specific accounts.

## Kafka topics

`kafka-init` runs `kafka/create-topics.sh` after Kafka becomes healthy. It creates
the following topics, each with three partitions and replication factor one:

- `knowledge.document.ingest`
- `knowledge.chunk.embed`
- `behavior.events`
- `review.events`

For a fresh database initialization, remove the named MySQL volume intentionally
before starting Compose again. Do not remove volumes when retaining local data.
