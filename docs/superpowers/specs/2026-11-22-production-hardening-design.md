# go-notify: production hardening and documentation

Date: 2026-11-22
Status: awaiting approval

## Intent

go-notify will run as an internal gRPC service. Other backend systems call it
to send notifications; it delivers them as push notifications through ntfy.
Several copies will run behind a load balancer.

Today the code works on a developer machine but is not safe to operate:
anyone who can reach the port can use it, a broken copy keeps receiving
traffic, there is almost nothing to observe, and nothing explains how to
call, configure or run it. This work closes those gaps and documents the
result.

## What the owner decided

- The service is called only by backend systems, over gRPC.
- Push through ntfy is the only delivery channel. No new providers.
- One shared ntfy topic for now. People read notifications in the ntfy app.
  Per-user delivery comes later, through the owner's frontend.
- A load balancer sits in front of the service.
- Approach 1 below: harden the existing architecture.
- Code is written to the Clean Code skill supplied by the owner
  (`/Users/sid/Downloads/SKILL.md`), and to test-first development.
- Integration tests run locally against Docker. Nothing is pushed to the
  remote until the owner says so.
- Test coverage is 100%.
- The GitHub Actions CI is improved.

Everything else in this document is the designer's call and is marked as such
where it matters. The owner can overrule any of it.

## Goals

1. A backend can call the service safely: authenticated, validated input,
   meaningful gRPC status codes.
2. A copy that cannot do its job stops receiving traffic.
3. No accepted notification is silently lost.
4. An operator can see what the service is doing and why something failed.
5. A new engineer can call, configure, run and change the service from the
   docs alone.

## Non-goals

- Email, SMS or any provider other than ntfy.
- Per-user ntfy topics or per-user delivery.
- TLS inside the service. It ends at the load balancer.
- Kubernetes manifests, Helm charts or infrastructure code. The deployment
  platform is not decided.
- Exactly-once delivery. The service is at-least-once; this is documented.
- A request idempotency key.
- Grafana dashboards and alert rules.

## Anti-goals

- Reporting a notification as `sent` when it was not delivered.
- Docs that describe behaviour the code does not have. The current README
  claims email/SMS support, Twilio and SendGrid; those claims go.
- A rewrite. The current architecture and the fixes already in the working
  tree stay.

## Constraints

- Go 1.23, the existing libraries (grpc, sqlx, streadway/amqp, go-redis v8,
  viper, zap, prometheus client). New runtime dependencies need a reason.
- Database changes go through new goose migrations in `db/migrations`. The
  existing migration is not edited.
- Proto changes must stay wire-compatible: fields are added, never renumbered
  or retyped.
- Docker Desktop is available on the development machine. Tests that need a
  real RabbitMQ, Postgres or Redis run there and in CI. Unit tests must not
  need them.
- The working tree already holds uncommitted bug fixes from the previous
  round (30 tests, all passing). This work builds on them.
- Where the Clean Code skill conflicts with Go idiom, Go idiom wins and the
  deviation is noted in the commit message. Known cases: errors are returned
  values, not exceptions; exported identifiers carry doc comments.

## Approaches

1. **Harden what exists (chosen).** Keep gRPC server → Postgres → RabbitMQ →
   worker → ntfy and fill the gaps listed under Design. Least risk; builds on
   the fixes already made. The window where a crash leaves a notification
   stuck in `pending` is repaired afterwards by a sweep, not prevented.
2. **Transactional outbox.** Write the notification and an outbox row in one
   transaction; a relay publishes from the table. Closes that window
   completely, at the cost of a new component to build and operate. Rejected:
   the sweep gets close for much less.
3. **Drop RabbitMQ, queue in Postgres.** Fewer moving parts for a single
   provider, but a rewrite that discards current work and makes later
   providers harder. Rejected.

The owner picked approach 1.

## Design

### API

- `SendNotification` accepts only `type = "push"`. `email` and `sms` are
  rejected with `InvalidArgument` and a message naming the supported types.
  The email and SMS queues stay declared in RabbitMQ so a later provider is a
  small change. (Designer's call.)
- Input limits, rejected with `InvalidArgument`: `user_id` required, at most
  255 bytes; `message` required, at most 4096 bytes (ntfy's message limit);
  `title` at most 250 bytes; `priority` empty or one of `1`–`5`.
- The proto gains comments on every service, method, message and field.
  Existing `success` and `error` fields stay for compatibility and are
  documented as deprecated in favour of gRPC status codes.

### Authentication (designer's call)

- Callers send an API key in the `x-api-key` gRPC metadata header.
- Keys are configured as a list of `name:key` pairs (`auth.apiKeys`, env
  `AUTH_APIKEYS`, comma-separated), one per calling backend, so a key can be
  rotated or revoked per caller and the caller's name appears in logs and
  metrics. Comparison is constant-time.
- A unary interceptor rejects missing or unknown keys with `Unauthenticated`.
  The gRPC health service is exempt so the load balancer can probe it.
- The server refuses to start with no keys unless `auth.disabled: true` is
  set explicitly. Local docker-compose sets a development key.
- Server reflection is controlled by `grpc.reflection`, default off.

### Health

- Server: the gRPC health service reports `SERVING` only while Postgres,
  Redis and RabbitMQ all respond. A background check runs every 10 seconds
  and flips the status. On shutdown the status goes `NOT_SERVING` first, then
  the server drains.
- Worker: an HTTP endpoint on its metrics port, `/healthz`, returns 200 while
  the broker connection is up and Postgres responds, otherwise 503.

### No silent loss

All of this section is the designer's call.

- Statuses become `pending` → `queued` → `sent` or `failed`:
  - `pending`: stored, not yet confirmed by the broker.
  - `queued`: the broker confirmed the publish; waiting for the worker.
  - `sent` / `failed`: final.
  The server moves a row to `queued` after the publish is confirmed. This is
  one extra database write per request and is what lets the sweep tell "never
  published" from "published and waiting". A worker outage or backlog leaves
  rows `queued`, which the sweep never touches.
- New migration adds to `notifications`: `title`, `type`, `priority`,
  `updated_at`, `sweep_attempts` (default 0); makes `status` `NOT NULL`;
  indexes `(status, created_at)`. The service has not run in production, so
  existing rows are development data: the migration gives them
  `type = 'push'`, empty title/priority, and marks any that are `pending` or
  NULL as `failed` so the first sweep does not push stale rows to the topic.
- The server stores the full notification, so it can be re-published.
- A sweep runs inside the server process every minute. It claims
  notifications that have been `pending` for more than 5 minutes
  (`FOR UPDATE SKIP LOCKED`, at most 100 per pass, so copies do not collide),
  re-publishes them, moves them to `queued` and increments `sweep_attempts`.
  Interval, age and batch size are configurable. A notification whose
  re-publish has failed 3 times is marked `failed`.
- The worker skips, and acknowledges, a message whose notification is already
  `sent` or `failed`. This covers the remaining duplicate case: a crash after
  the broker confirmed but before the row became `queued`.
- A publish failure during a request still marks the row `failed` and returns
  `Unavailable`; the caller is told and retries.
- The worker stops consuming the dead-letter queue. Today it logs and
  discards those messages; instead they stay in the queue for inspection and
  replay, and a metric counts them. (Designer's call.)

### ntfy (designer's call)

- The base URL is configurable, default `https://ntfy.sh`, so a self-hosted
  server can be used without a code change.
- An optional access token (`ntfy.token`, env `NTFY_TOKEN`) is sent as a
  bearer token when set. It is never logged.
- A `429` from ntfy is treated as "slow down", not as a failed send: the
  worker waits for the `Retry-After` time (capped at one minute) before the
  next attempt. Attempts are still bounded; after the last one the
  notification is `failed` and dead-lettered as usual.

### Configuration

- `config.Load` returns `(*Config, error)`. The global `AppConfig` goes;
  each `main` passes what each component needs.
- Validation at startup, with every problem reported in one error: required
  values present, durations positive, ports and URLs well-formed, rate limit
  positive.
- `redis.window` becomes a duration like the Postgres ones.
- The worker's hardcoded values (worker count, send attempts, retry delay,
  send timeout) and the ntfy base URL move into config with the current
  values as defaults.
- Key names keep their current spelling so existing env vars keep working.

### Logging

- One zap logger per process, built from `logging.level`, passed to
  components. No `log` or `println` anywhere.
- A unary interceptor logs each request: method, caller name, status code,
  duration. Notification bodies are not logged.
- Every log line about a notification carries `notification_id`.

### Metrics

- Server: requests by method, caller and status code; request duration;
  rate-limited requests; sweep re-publishes and sweep failures. The existing
  `notifications_received_total` stays.
- Worker (new metrics endpoint): sends by type and outcome; send duration;
  dead-lettered messages; broker reconnects.
- Metrics are registered on an injected registry, not in `init()`.

### Tests and coverage

- Unit tests need no external services and run with the race detector.
- Integration tests carry the `integration` build tag and run against real
  RabbitMQ, Postgres and Redis started by a compose file used both locally
  (`make test-integration`) and in CI, with ntfy replaced by a local stub
  server. They cover: publish → consume → stub → status `sent`;
  dead-lettering; recovery after a broker restart; publisher confirms;
  graceful shutdown; the sweep; the rate limiter; migrations up and down.
- Coverage is 100% of statements, measured over unit and integration runs
  combined, and enforced by a check that fails below 100%. What "100%"
  covers is the designer's call:
  - Counted: every package under `internal/` and `config/`.
  - Not counted: generated protobuf code (`api/generated`) and the two
    `main` functions. To keep that exclusion honest, each `main` only loads
    config, calls a `Run` function in a counted package and exits with its
    error; all wiring lives in counted code.
  - No coverage-exclusion comments. A statement that cannot be reached by a
    test is removed or restructured, not hidden.
- Tests assert behaviour. A test written only to execute a line, with no
  meaningful assertion, does not count as covering it.

### Containers

- Dockerfiles: pinned base images, non-root user, distroless runtime image,
  health probe fetched with a checksum and for the build architecture.
- docker-compose stays the local development stack: restart policies, health
  checks on Postgres and Redis, the worker's metrics port, no `latest` tags.

### CI (designer's call on the details)

Separate jobs, so a failure names its cause and independent checks run in
parallel:

- **lint**: `golangci-lint` with a committed config, `gofmt`, `go vet`.
- **generate**: regenerates protobuf code with pinned tool versions; fails
  if `go mod tidy` would change `go.mod` or `go.sum`.
- **unit**: unit tests with the race detector.
- **integration**: starts the compose services, applies migrations, runs the
  `integration` tests.
- **coverage**: merges both profiles and fails below 100%; uploads the
  report as an artifact.
- **security**: `govulncheck` on the module.
- **images**: builds both images on every run to catch Dockerfile breakage;
  pushes only on `main`, tagged `${DOCKER_USERNAME}/go-notify-server` and
  `${DOCKER_USERNAME}/go-notify-worker` with the commit SHA and `latest`,
  using the Docker Hub secrets already configured.

Across the workflow: actions pinned to exact versions, `permissions` set to
read-only by default, Go version read from `go.mod`, module and build caches
through `setup-go`, a concurrency group that cancels superseded runs, and a
timeout on every job.

### Documentation

- `README.md`: what it is, how it works in one diagram, five-minute
  quickstart including a real `grpcurl` call, links to the rest. Claims match
  the code.
- `docs/architecture.md`: components, the life of a notification, statuses,
  delivery guarantee, failure modes and what happens in each.
- `docs/api.md`: both methods, fields, limits, status codes, authentication,
  examples.
- `docs/configuration.md`: every key, env var, default and meaning. A test
  fails if a config key is missing from this file.
- `docs/operations.md`: running behind a load balancer, health checks,
  migrations, metrics and what to alert on, reading logs, inspecting and
  replaying the dead-letter queue, key rotation, the shared-topic privacy
  caveat, shutdown behaviour.
- `docs/development.md`: prerequisites, proto generation, tests, lint,
  project layout, how to add a provider.
- `CONTRIBUTING.md`: corrected to match the real build commands.
- A package comment for every Go package.

## Left to the builder

- Package and file layout, names, and how the work is split into commits.
- Metric names and label sets, within Prometheus naming conventions.
- The exact shape of the health-check and sweep components.
- Wording and structure inside each document.
- Whether integration tests use plain service containers or a helper library.

## Known risk

The RabbitMQ reconnect, publisher-confirm and graceful-shutdown code from the
previous round has never run against a real broker. The integration tests
are where it first gets exercised. If it fails there, fixing it is part of
this work.

The improved CI workflow itself cannot be run until a branch is pushed, which
waits for the owner. Until then it is checked only by reading and by running
the same `make` targets locally.
