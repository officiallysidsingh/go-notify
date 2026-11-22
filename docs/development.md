# Development

## Prerequisites

- Go 1.26+
- Docker Desktop (for compose and integration tests)
- `golangci-lint` v2
- `protoc` with `protoc-gen-go` and `protoc-gen-go-grpc` (for `make proto`)
- `grpcurl`
- `govulncheck`
- `actionlint`
- `goose` (optional, for `make migrate-up` / `migrate-down`)

## Make targets

| Target | What it does |
|---|---|
| `make build` | build `bin/server` and `bin/worker` |
| `make run` / `make worker` | build and run the server / worker |
| `make proto` | regenerate `api/generated/` from `api/proto/*.proto` |
| `make test` | `go test -race ./...`, no external services |
| `make test-services-up` / `-down` | start / stop Postgres, RabbitMQ and Redis for tests |
| `make test-integration` | integration tests against the test services |
| `make coverage` | full run with integration tests; fails below 100% |
| `make lint` | gofmt check, `go vet`, `golangci-lint` |
| `make tidy-check` | fail if `go.mod`/`go.sum` are not tidy |
| `make docker-build` / `docker-up` / `docker-down` | build, start, stop the compose stack |
| `make migrate-up` / `migrate-down` | apply / roll back migrations with goose |
| `make restart-rabbitmq` | restart the RabbitMQ container |
| `make clean` | remove `bin/` |

## Protobuf

Edit `api/proto/notify.proto`, then run `make proto`. Never renumber or retype
existing fields.

## Tests

- Unit tests (`make test`) need no services.
- Integration tests (`make test-integration`) use build tag `integration`, run
  from `internal/integration/`, and need Docker.

## Coverage rule

Statement coverage over `config/` and `internal/` must be 100%. Run
`make coverage`; `scripts/check-coverage.sh` ignores test-support packages
(`rabbitmqtest`, `integration`), prints per-function coverage and fails if any
statement is uncovered. Add tests with new code rather than excluding it.

## Project layout

| Path | Purpose |
|---|---|
| `cmd/server`, `cmd/worker` | process entry points |
| `api/proto`, `api/generated` | protobuf definition and generated code |
| `config/` | configuration loading and validation |
| `internal/app` | wiring for server and worker |
| `internal/grpc` | RPC handlers, validation, interceptors |
| `internal/auth` | API key authentication |
| `internal/ratelimiter` | Redis rate limiter |
| `internal/repository` | Postgres access |
| `internal/producer` | publishes to RabbitMQ |
| `internal/consumer` | worker consumer and message processor |
| `internal/service` | providers that send notifications (ntfy) |
| `internal/sweeper` | republishes stuck `pending` rows |
| `internal/rabbitmq` | topology and AMQP interfaces (`rabbitmqtest` has fakes) |
| `internal/health`, `internal/metrics`, `internal/logging` | observability |
| `internal/notification` | message type, statuses, supported types |
| `internal/integration` | integration tests |
| `db/migrations` | goose migrations |
| `deployments/` | compose stacks, Dockerfiles, Prometheus and Loki config |

## Adding a provider

1. Implement `consumer.Sender` in `internal/service`.
2. Register it in `app.RunWorker`.
3. Add the type to `notification.SupportedTypes()`.
4. Add tests covering the sender and the processor's dispatch.
