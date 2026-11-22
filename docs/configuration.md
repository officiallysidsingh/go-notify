# Configuration

Configuration is read from `config.yaml` (searched in the directory the process
is given; see `config/config.example.yaml`) and overridden by environment
variables. The environment variable for a key is the key uppercased with dots
replaced by underscores, for example `grpc.port` becomes `GRPC_PORT`.

A dash in the Default column means there is no built-in default; the value must
be supplied. "Required for" says which process reads the key.

| Key | Env var | Default | Required for | Meaning |
|---|---|---|---|---|
| `grpc.port` | `GRPC_PORT` | `:50051` | server | gRPC listener address |
| `grpc.reflection` | `GRPC_REFLECTION` | `false` | server | enable server reflection (dev only) |
| `auth.apiKeys` | `AUTH_APIKEYS` | `""` | server | comma-separated `name:key` entries |
| `auth.disabled` | `AUTH_DISABLED` | `false` | server | if true, skip auth (dev only) |
| `logging.level` | `LOGGING_LEVEL` | `info` | both | debug/info/warn/error |
| `ntfy.baseURL` | `NTFY_BASEURL` | `https://ntfy.sh` | worker | ntfy server |
| `ntfy.topic` | `NTFY_TOPIC` | `""` | worker | ntfy topic to publish to |
| `ntfy.token` | `NTFY_TOKEN` | `""` | worker | optional bearer token |
| `ntfy.timeout` | `NTFY_TIMEOUT` | `10s` | worker | HTTP timeout for ntfy requests |
| `rabbitmq.url` | `RABBITMQ_URL` | — | both | AMQP URL |
| `postgres.DataSourceName` | `POSTGRES_DATASOURCENAME` | — | both | PostgreSQL DSN |
| `postgres.MaxOpenConns` | `POSTGRES_MAXOPENCONNS` | — | both | pool size |
| `postgres.MaxIdleConns` | `POSTGRES_MAXIDLECONNS` | — | both | pool size |
| `postgres.ConnMaxLifetime` | `POSTGRES_CONNMAXLIFETIME` | — | both | connection recycling |
| `postgres.ConnMaxIdleTime` | `POSTGRES_CONNMAXIDLETIME` | — | both | idle recycling |
| `postgres.ConnTimeout` | `POSTGRES_CONNTIMEOUT` | `5s` | both | DB dial/ping timeout |
| `redis.addr` | `REDIS_ADDR` | — | server | Redis address |
| `redis.limit` | `REDIS_LIMIT` | — | server | rate-limit max requests per window |
| `redis.window` | `REDIS_WINDOW` | — | server | rate-limit window duration |
| `metrics.port` | `METRICS_PORT` | — | server | Prometheus scrape port |
| `worker.concurrency` | `WORKER_CONCURRENCY` | `10` | worker | parallel send workers |
| `worker.sendAttempts` | `WORKER_SENDATTEMPTS` | `3` | worker | retry attempts per message |
| `worker.retryDelay` | `WORKER_RETRYDELAY` | `2s` | worker | delay between retries |
| `worker.metricsPort` | `WORKER_METRICSPORT` | `:9092` | worker | HTTP port for /metrics and /healthz |
| `sweep.interval` | `SWEEP_INTERVAL` | `1m` | server | sweep tick |
| `sweep.staleAfter` | `SWEEP_STALEAFTER` | `5m` | server | age before a pending row is swept |
| `sweep.batchSize` | `SWEEP_BATCHSIZE` | `100` | server | rows per sweep pass |
| `sweep.maxAttempts` | `SWEEP_MAXATTEMPTS` | `3` | server | sweeps before marking failed |
| `health.interval` | `HEALTH_INTERVAL` | `10s` | both | health check tick |

Durations use Go syntax (`500ms`, `10s`, `5m`).

## Secrets

Treat these as secrets: keep them out of version control and logs, and inject
them through environment variables or your secret manager.

- `auth.apiKeys`: every key grants full access to the API.
- `ntfy.token`: bearer token for the ntfy server.
- `postgres.DataSourceName`: the DSN contains the database password.
- `rabbitmq.url`: the AMQP URL contains the broker password.
