# go-notify Production Hardening Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make go-notify safe to run as an internal gRPC notification service behind a load balancer, with 100% test coverage, a restructured CI and complete documentation.

**Architecture:** The existing flow stays: gRPC server → Postgres → RabbitMQ → worker → ntfy. Each process gets one `Run` function in `internal/app` that wires injected components (config, logger, metrics registry); `main` only loads config and calls it. Third-party clients sit behind small interfaces so every error branch is reachable from a unit test, and a compose-backed integration suite exercises the real services.

**Tech Stack:** Go 1.23, grpc-go, sqlx + lib/pq, streadway/amqp, go-redis v8, viper, zap, prometheus client, goose migrations, testify, sqlmock, miniredis, Docker Compose, GitHub Actions.

**Spec:** `docs/superpowers/specs/2026-11-22-production-hardening-design.md`

## Global Constraints

- Go 1.23 and the existing libraries. New runtime dependencies need a reason stated in the commit message. New test-only dependency allowed: `github.com/pressly/goose/v3` (run migrations from tests).
- Database changes go through new goose migrations in `db/migrations`. `20250207175052_init_schema.sql` is not edited.
- Proto changes stay wire-compatible: fields are added, never renumbered or retyped.
- Unit tests need no external services. Tests that do carry `//go:build integration`.
- Coverage is 100% of statements over `./config/...` and `./internal/...`, unit and integration runs combined. Not counted: `api/generated`, `cmd/*/main.go`. No coverage-exclusion comments; an unreachable statement is restructured or removed. A test with no meaningful assertion does not count.
- Clean Code skill at `/Users/sid/Downloads/SKILL.md` governs all code. Go idiom wins where they conflict (returned errors, doc comments on exported identifiers); note each deviation in the commit message.
- Test-first for every behaviour change: write the test, watch it fail for the expected reason, then implement.
- No `log`, `fmt.Print*` or `println` in non-test code after Task 13. One injected `*zap.Logger` per process.
- Existing config key spellings and env var names keep working.
- Nothing is pushed to the remote. All commits are local, on branch `production-hardening`.
- Statuses are exactly `pending`, `queued`, `sent`, `failed`.
- Run before every commit: `gofmt -l . && go vet ./... && go test -race ./...` — all clean.

## File Structure

| Path | Responsibility |
|---|---|
| `config/config.go` | Typed config, `Load`, defaults |
| `config/validate.go` | `ValidateServer`, `ValidateWorker` |
| `internal/logging/logging.go` | Build the zap logger from a level name |
| `internal/notification/notification.go` | Message, statuses, supported types (exists) |
| `internal/repository/db.go` | Connection, `Ping`, `Close` |
| `internal/repository/notifications.go` | All notification queries |
| `internal/auth/keyring.go` | Parse and match API keys |
| `internal/auth/interceptor.go` | Unary interceptor, caller in context |
| `internal/metrics/server.go`, `worker.go` | Metric sets built on an injected registry |
| `internal/grpc/server.go` | The two RPC handlers |
| `internal/grpc/validation.go` | Request validation |
| `internal/grpc/interceptors.go` | Request logging and request metrics |
| `internal/rabbitmq/topology.go` | Exchange/queue layout (exists) |
| `internal/rabbitmq/amqp.go` | `Channel`, `Connection`, `Dialer` interfaces and the real adapter |
| `internal/producer/producer.go` | Confirmed publish, reconnect, `Ping` |
| `internal/sweeper/sweeper.go` | Re-publish stale `pending` notifications |
| `internal/service/ntfy.go` | ntfy sender, token, 429 handling |
| `internal/consumer/processor.go` | Deliver one notification and settle it |
| `internal/consumer/consumer.go` | AMQP subscription, worker pool, reconnect |
| `internal/health/health.go` | Run named checks on an interval, report changes |
| `internal/app/server.go`, `worker.go` | Wire and run each process |
| `cmd/server/main.go`, `cmd/worker/main.go` | Load config, call `app.Run*`, exit |
| `internal/integration/*_test.go` | Integration suite and its harness |
| `deployments/docker-compose.test.yaml` | RabbitMQ, Postgres, Redis for tests |
| `scripts/check-coverage.sh` | Fail below 100% |
| `.golangci.yaml`, `.github/workflows/ci.yaml` | Lint config, CI |
| `docs/*.md`, `README.md`, `CONTRIBUTING.md` | Documentation |

## Review Focus

Failure modes the spec implies but does not spell out. Each is pinned by a test in the task named.

1. **Redis is down.** Requests fail with `Unavailable` (never silently unlimited) and health goes `NOT_SERVING`. → Task 7 (`TestSendNotificationFailsClosedWhenRateLimiterIsDown`), Task 12.
2. **Title is not a legal HTTP header value** (newline, control character, non-ASCII). ntfy takes the title as a header; a newline would make every send fail and dead-letter. Control characters are rejected at the API; non-ASCII titles are RFC 2047 encoded by the sender. → Task 4, Task 10.
3. **Malformed `AUTH_APIKEYS`**: empty entries, surrounding spaces, a key containing `:`, duplicate names, empty key. Startup fails with a message naming the entry, never a silently empty key ring. → Task 5.
4. **429 with a missing or unparseable `Retry-After`.** The worker falls back to the configured retry delay rather than not waiting or waiting forever. → Task 10.
5. **Worker receives a message whose notification row does not exist.** It dead-letters without sending, rather than sending something the database has no record of. → Task 11.

---

### Task 0: Baseline branch

**Files:** none new.

- [ ] **Step 1:** `git checkout -b production-hardening`
- [ ] **Step 2:** Confirm green: `go build ./... && go vet ./... && go test -race ./...` → all `ok`.
- [ ] **Step 3:** Commit the previous round's work, excluding `docs/superpowers`:

```bash
git add -A ':!docs/superpowers'
git commit -m "fix: delivery, API, rate limiter, config and CI bugs from code review"
git add docs/superpowers
git commit -m "docs: production hardening design and plan"
```

---

### Task 1: Config without globals, with validation

**Files:**
- Modify: `config/config.go`, `config/config.example.yaml`, `config/config_test.go`
- Create: `config/validate.go`, `config/validate_test.go`
- Modify (compile only): `cmd/server/main.go`, `cmd/worker/main.go`

**Interfaces — Produces:**

```go
func Load(path string) (*Config, error) // missing file is fine (env only); unreadable/invalid file is an error
func (c *Config) ValidateServer() error // errors.Join of every problem
func (c *Config) ValidateWorker() error

type Config struct {
	GRPC     GRPCConfig     // Port string; Reflection bool
	Auth     AuthConfig     // APIKeys string; Disabled bool
	RabbitMQ RabbitMQConfig // URL string
	Postgres PostgresConfig // unchanged fields
	Redis    RedisConfig    // Addr string; Limit int; Window time.Duration
	Metrics  MetricsConfig  // Port string
	Logging  LoggingConfig  // Level string
	Ntfy     NtfyConfig     // BaseURL, Topic, Token string; Timeout time.Duration
	Worker   WorkerConfig   // Concurrency, SendAttempts int; RetryDelay time.Duration; MetricsPort string
	Sweep    SweepConfig    // Interval, StaleAfter time.Duration; BatchSize, MaxAttempts int
	Health   HealthConfig   // Interval time.Duration
}
```

Keys and defaults (new ones; existing keys unchanged):

| Key | Default |
|---|---|
| `grpc.reflection` | `false` |
| `auth.apiKeys` | `""` |
| `auth.disabled` | `false` |
| `logging.level` | `info` |
| `ntfy.baseURL` | `https://ntfy.sh` |
| `ntfy.token` | `""` |
| `ntfy.timeout` | `10s` |
| `worker.concurrency` | `10` |
| `worker.sendAttempts` | `3` |
| `worker.retryDelay` | `2s` |
| `worker.metricsPort` | `:9092` |
| `sweep.interval` | `1m` |
| `sweep.staleAfter` | `5m` |
| `sweep.batchSize` | `100` |
| `sweep.maxAttempts` | `3` |
| `health.interval` | `10s` |
| `postgres.ConnTimeout` | `5s` (exists) |

Validation rules. Server: `grpc.port`, `metrics.port`, `rabbitmq.url`, `postgres.DataSourceName`, `redis.addr` non-empty; `rabbitmq.url` parses with scheme `amqp` or `amqps`; `redis.limit > 0`; `redis.window`, sweep and health durations `> 0`; `sweep.batchSize`, `sweep.maxAttempts > 0`; `auth.apiKeys` non-empty unless `auth.disabled`. Worker: `rabbitmq.url`, `postgres.DataSourceName`, `ntfy.topic`, `worker.metricsPort` non-empty; `ntfy.baseURL` parses with scheme `http`/`https`; `worker.concurrency`, `worker.sendAttempts > 0`; `worker.retryDelay`, `ntfy.timeout > 0`.

- [ ] **Step 1: Write failing tests**

```go
// config_test.go — replace the AppConfig-based tests
func TestExampleConfigDefinesPostgresDurations(t *testing.T)   // via Load(dir); same three assertions as today
func TestLoadAppliesDefaults(t *testing.T) {
	cfg, err := Load(t.TempDir())
	require.NoError(t, err)
	assert.Equal(t, "https://ntfy.sh", cfg.Ntfy.BaseURL)
	assert.Equal(t, 5*time.Minute, cfg.Sweep.StaleAfter)
	assert.Equal(t, 10, cfg.Worker.Concurrency)
	assert.False(t, cfg.GRPC.Reflection)
}
func TestLoadReadsEnvironmentOverrides(t *testing.T)   // t.Setenv("AUTH_APIKEYS","billing:k1") → cfg.Auth.APIKeys
func TestLoadRejectsMalformedFile(t *testing.T)        // config.yaml = "grpc: [" → error
func TestExampleConfigIsValidForServerAndWorker(t *testing.T) // both Validate* return nil

// validate_test.go
func TestValidateServerReportsEveryProblem(t *testing.T) {
	err := (&Config{}).ValidateServer()
	assert.ErrorContains(t, err, "grpc.port")
	assert.ErrorContains(t, err, "rabbitmq.url")
	assert.ErrorContains(t, err, "auth.apiKeys")
}
func TestValidateServerAllowsMissingKeysWhenAuthDisabled(t *testing.T)
func TestValidateServerRejectsNonAMQPURL(t *testing.T)   // "http://x" → ErrorContains "rabbitmq.url"
func TestValidateWorkerRequiresNtfyTopic(t *testing.T)
func TestValidateWorkerRejectsNonPositiveConcurrency(t *testing.T)
```

- [ ] **Step 2:** `go test ./config/` → FAIL (undefined `Load`, `ValidateServer`).
- [ ] **Step 3:** Implement. Remove `AppConfig` and `LoadConfig`. Add every new key to `config.example.yaml` with a one-line comment; set `auth.apiKeys: "local-dev:change-me"`.
- [ ] **Step 4:** Update both `main.go` files only enough to compile against `Load` (full rewrite is Task 13).
- [ ] **Step 5:** `go test -race ./config/ && go build ./...` → PASS.
- [ ] **Step 6:** Commit `refactor(config): return config from Load and validate at startup`.

---

### Task 2: Logger

**Files:** Create `internal/logging/logging.go`, `internal/logging/logging_test.go`.

**Interfaces — Produces:** `func New(level string) (*zap.Logger, error)` — production JSON encoder, level parsed with `zapcore.ParseLevel`; unknown level is an error naming the value.

- [ ] **Step 1: Tests**

```go
func TestNewEnablesConfiguredLevel(t *testing.T) {
	logger, err := New("warn")
	require.NoError(t, err)
	assert.False(t, logger.Core().Enabled(zapcore.InfoLevel))
	assert.True(t, logger.Core().Enabled(zapcore.WarnLevel))
}
func TestNewRejectsUnknownLevel(t *testing.T) // New("loud") → ErrorContains "loud"
```

- [ ] **Step 2:** Run → FAIL. **Step 3:** Implement. **Step 4:** Run → PASS.
- [ ] **Step 5:** Commit `feat(logging): build zap logger from configured level`.

---

### Task 3: Schema and repository

**Files:**
- Create: `db/migrations/20261122120000_notification_delivery_fields.sql`, `internal/repository/notifications.go`, `internal/repository/notifications_test.go`
- Modify: `internal/repository/db.go`, `internal/repository/db_test.go`, `internal/notification/notification.go`

**Migration (Up):** add `title TEXT NOT NULL DEFAULT ''`, `type TEXT NOT NULL DEFAULT 'push'`, `priority TEXT NOT NULL DEFAULT ''`, `updated_at TIMESTAMPTZ NOT NULL DEFAULT now()`, `sweep_attempts INTEGER NOT NULL DEFAULT 0`; `UPDATE notifications SET status='failed' WHERE status IS NULL OR status='pending'`; `ALTER COLUMN status SET NOT NULL`; `CREATE INDEX notifications_status_updated_at_idx ON notifications (status, updated_at)`. **Down** reverses all of it. (The index is on `updated_at`, not `created_at` as the spec says, because the sweep ages rows by `updated_at`; note this in the commit message.)

**Interfaces — Produces:**

```go
// notification package
const StatusQueued = "queued"
func IsSettled(status string) bool // sent or failed

// repository package
type StaleNotification struct {
	notification.Message
	SweepAttempts int
}
func NewDB(cfg config.PostgresConfig) (*DB, error)
func (d *DB) Ping(ctx context.Context) error
func (d *DB) InsertNotification(ctx context.Context, message notification.Message) (int64, error) // status pending; stores user_id,title,type,priority,message
func (d *DB) MarkQueued(ctx context.Context, id int64) error            // pending → queued only; no error if already past pending
func (d *DB) UpdateNotificationStatus(ctx context.Context, id int64, status string) error // also sets updated_at
func (d *DB) GetNotificationStatus(ctx context.Context, id int64) (string, error)
func (d *DB) ClaimStalePending(ctx context.Context, staleAfter time.Duration, limit int) ([]StaleNotification, error)
```

`ClaimStalePending` is one statement so concurrent copies never claim the same row, and bumping `updated_at` keeps a claimed row out of the stale window:

```sql
UPDATE notifications
SET sweep_attempts = sweep_attempts + 1, updated_at = now()
WHERE id IN (
	SELECT id FROM notifications
	WHERE status = 'pending' AND updated_at < now() - $1::interval
	ORDER BY id LIMIT $2
	FOR UPDATE SKIP LOCKED
)
RETURNING id, user_id, title, priority, message, type, sweep_attempts
```

`MarkQueued`: `UPDATE ... SET status='queued', updated_at=now() WHERE id=$1 AND status='pending'`.

- [ ] **Step 1: Tests (sqlmock)** — keep the four existing tests, updated to the new `InsertNotification` signature, and add:

```go
func TestInsertNotificationStoresDeliveryFields(t *testing.T)  // WithArgs("user-1","Title","push","3","hello","pending")
func TestMarkQueuedOnlyMovesPendingNotifications(t *testing.T) // ExpectExec regexp `status = 'queued'.*status = 'pending'`
func TestClaimStalePendingReturnsClaimedNotifications(t *testing.T) {
	// rows: (7,"user-1","T","3","hello","push",2) → len 1, NotificationID 7, SweepAttempts 2
}
func TestClaimStalePendingPassesAgeAndLimit(t *testing.T)      // WithArgs("300 seconds", 100) for 5m / 100
func TestPingReportsUnreachableDatabase(t *testing.T)          // sqlmock MonitorPingsOption + ExpectPing().WillReturnError
// One test per query for the driver-error branch, asserting the wrapped message names the operation.
```

- [ ] **Step 2:** Run → FAIL. **Step 3:** Implement; move the three query methods from `db.go` to `notifications.go`. Make `NewDB` take an unexported `open func(driver, dsn string) (*sqlx.DB, error)` seam (or equivalent) so its error branches are testable.
- [ ] **Step 4:** `go test -race ./internal/repository/ ./internal/notification/` → PASS. Fix `internal/grpc` callers enough to compile.
- [ ] **Step 5:** Commit `feat(repository): store full notification, queued status and stale-pending claim`.

---

### Task 4: Request validation

**Files:** Create `internal/grpc/validation.go`, `internal/grpc/validation_test.go`. Modify `internal/grpc/server.go` (remove the old `validate`), `internal/notification/notification.go`.

**Interfaces — Produces:**

```go
// notification
func SupportedTypes() []string       // []string{TypePush}
func IsSupportedType(t string) bool  // push only; Types() (all three) stays for the queue layout

// grpc
const (
	maxUserIDBytes  = 255
	maxMessageBytes = 4096
	maxTitleBytes   = 250
)
func validateSend(req *pb.NotificationRequest) error   // status error, codes.InvalidArgument
func validateStatus(req *pb.StatusRequest) error        // notification_id must be > 0
```

Limits are in bytes (`len`). `priority` is `""` or `"1"`–`"5"`. `title` and `user_id` reject any rune where `unicode.IsControl` is true.

- [ ] **Step 1: Tests** — table-driven, one row per rule, each asserting `codes.InvalidArgument` and that the message names the field:

```go
func TestValidateSendRejects(t *testing.T) // rows: empty user_id; user_id 256 bytes; empty message; message 4097 bytes;
	// title 251 bytes; title "a\nb"; priority "6"; priority "high"; type "email"; type "sms"; type ""
func TestValidateSendAcceptsLimits(t *testing.T) // user_id 255, message 4096, title 250, priority "5", non-ASCII title "Größe" → nil
func TestValidateSendCountsBytesNotCharacters(t *testing.T) // message = 2000 × "é" (4000 bytes) ok; 2049 × "é" rejected
func TestValidateSendNamesSupportedTypes(t *testing.T)      // type "email" → ErrorContains "push"
func TestValidateStatusRejectsNonPositiveID(t *testing.T)   // 0 and -1
```

- [ ] **Step 2–4:** fail, implement, pass. Update `server_test.go`: the `fax` test still passes; add `TestSendNotificationRejectsEmailBeforeStoring`.
- [ ] **Step 5:** Commit `feat(grpc): validate request fields and accept push only`.

---

### Task 5: API-key authentication

**Files:** Create `internal/auth/keyring.go`, `interceptor.go`, and tests for each.

**Interfaces — Produces:**

```go
const MetadataKey = "x-api-key"
type KeyRing struct{ /* unexported */ }
func ParseKeys(raw string) (KeyRing, error)              // "name:key,name:key"; key is everything after the FIRST colon
func (k KeyRing) Caller(key string) (name string, known bool) // constant-time over every entry
func UnaryInterceptor(keys KeyRing, exemptMethods ...string) grpc.UnaryServerInterceptor
func AllowAll() grpc.UnaryServerInterceptor               // auth.disabled; caller name "anonymous"
func CallerFromContext(ctx context.Context) string        // "" when none
```

`ParseKeys` trims spaces around entries and names; errors on: empty input, an entry without `:`, empty name, empty key, duplicate name, duplicate key. The error names the entry by name or position, never prints a key.

- [ ] **Step 1: Tests**

```go
func TestParseKeysAcceptsMultipleCallers(t *testing.T)      // "billing:k1, orders:k2" → Caller("k2") == "orders", true
func TestParseKeysKeepsColonsInsideKey(t *testing.T)        // "billing:a:b" → Caller("a:b") known
func TestParseKeysRejects(t *testing.T)                     // rows: "", "billing", ":k", "billing:", "a:k,a:j", "a:k,b:k", "a:k,,b:j"
func TestParseKeysErrorDoesNotRevealKey(t *testing.T)       // "a:secret,a:other" → NotContains "secret"
func TestCallerRejectsUnknownKey(t *testing.T)
func TestInterceptorRejectsMissingKey(t *testing.T)         // codes.Unauthenticated; handler not called
func TestInterceptorRejectsUnknownKey(t *testing.T)
func TestInterceptorPassesCallerToHandler(t *testing.T)     // CallerFromContext == "billing"
func TestInterceptorSkipsExemptMethod(t *testing.T)         // "/grpc.health.v1.Health/Check" with no key → handler called
func TestAllowAllNamesCallerAnonymous(t *testing.T)
```

- [ ] **Step 2–4:** fail, implement (`crypto/subtle.ConstantTimeCompare`), pass.
- [ ] **Step 5:** Commit `feat(auth): API-key authentication interceptor`.

---

### Task 6: Metrics and request interceptors

**Files:** Create `internal/metrics/server.go`, `worker.go`, tests; `internal/grpc/interceptors.go`, `interceptors_test.go`.

**Interfaces — Produces:**

```go
// metrics
type Server struct {
	Received        prometheus.Counter       // notifications_received_total (existing name)
	Requests        *prometheus.CounterVec   // gonotify_grpc_requests_total{method,caller,code}
	RequestDuration *prometheus.HistogramVec // gonotify_grpc_request_duration_seconds{method}
	RateLimited     prometheus.Counter       // gonotify_rate_limited_total
	SweepRepublished prometheus.Counter      // gonotify_sweep_republished_total
	SweepFailures   prometheus.Counter       // gonotify_sweep_failures_total
}
func NewServer(registerer prometheus.Registerer) *Server
type Worker struct {
	Sends          *prometheus.CounterVec   // gonotify_sends_total{type,outcome}  outcome: sent|failed|skipped
	SendDuration   *prometheus.HistogramVec // gonotify_send_duration_seconds{type}
	DeadLettered   prometheus.Counter       // gonotify_dead_lettered_total
	Reconnects     prometheus.Counter       // gonotify_broker_reconnects_total
}
func NewWorker(registerer prometheus.Registerer) *Worker

// grpc
func MetricsInterceptor(m *metrics.Server) grpc.UnaryServerInterceptor
func LoggingInterceptor(logger *zap.Logger) grpc.UnaryServerInterceptor // fields: method, caller, code, duration; never the request body
```

- [ ] **Step 1: Tests** (use `prometheus.NewRegistry()` and `testutil.ToFloat64`; logs via `zaptest/observer`)

```go
func TestNewServerRegistersOnGivenRegistry(t *testing.T)     // Gather() contains notifications_received_total after one Inc
func TestNewServerTwiceOnSeparateRegistriesDoesNotPanic(t *testing.T)
func TestMetricsInterceptorCountsRequestByCallerAndCode(t *testing.T) // handler returns NotFound, caller "billing" → counter{...,code="NotFound"} == 1
func TestMetricsInterceptorObservesDuration(t *testing.T)
func TestLoggingInterceptorLogsMethodCallerAndCode(t *testing.T)
func TestLoggingInterceptorDoesNotLogMessageBody(t *testing.T) // request message "top-secret" → no log entry contains it
func TestLoggingInterceptorLogsFailuresAtWarn(t *testing.T)    // OK → Info; any other code → Warn
```

- [ ] **Step 2–4:** fail, implement, pass. Delete the `init()` registration and package-level counter from `server.go`.
- [ ] **Step 5:** Commit `feat(metrics): request, sweep and worker metrics on an injected registry`.

---

### Task 7: gRPC handlers on the new store

**Files:** Modify `internal/grpc/server.go`, `server_test.go`.

**Interfaces:**
- Consumes: `repository` methods (Task 3), `validateSend`/`validateStatus` (Task 4), `metrics.Server` (Task 6).
- Produces:

```go
type NotificationStore interface {
	InsertNotification(ctx context.Context, message notification.Message) (int64, error)
	MarkQueued(ctx context.Context, id int64) error
	UpdateNotificationStatus(ctx context.Context, id int64, status string) error
	GetNotificationStatus(ctx context.Context, id int64) (string, error)
}
type Dependencies struct {
	Publisher   Publisher
	Store       NotificationStore
	RateLimiter RateLimiter
	Metrics     *metrics.Server
	Logger      *zap.Logger
}
func NewNotificationServer(deps Dependencies) *NotificationServer
```

Flow: validate → rate limit (count `RateLimited`) → insert `pending` → publish → `MarkQueued` → respond. A `MarkQueued` failure is logged and the request still succeeds (the sweep will re-publish; the worker's settled check absorbs the duplicate).

- [ ] **Step 1: Tests** — port the nine existing tests to `Dependencies`, then add:

```go
func TestSendNotificationMarksNotificationQueuedAfterPublish(t *testing.T) // store status == "queued"
func TestSendNotificationSucceedsWhenMarkQueuedFails(t *testing.T)         // response.Success, status stays "pending"
func TestSendNotificationStoresTitleTypeAndPriority(t *testing.T)
func TestSendNotificationFailsClosedWhenRateLimiterIsDown(t *testing.T)    // codes.Unavailable; nothing inserted
func TestSendNotificationCountsRateLimitedRequests(t *testing.T)
func TestGetNotificationStatusRejectsNonPositiveID(t *testing.T)
func TestGetNotificationStatusHidesStorageErrorDetails(t *testing.T)       // codes.Internal, NotContains driver text
func TestSendNotificationLogsCarryNotificationID(t *testing.T)             // publish fails → the Error entry has field notification_id == 7
```

- [ ] **Step 2–4:** fail, implement, pass. No `log` left in the package.
- [ ] **Step 5:** Commit `feat(grpc): queue state transition and injected dependencies`.

---

### Task 8: AMQP seam, producer `Ping`

**Files:** Create `internal/rabbitmq/amqp.go`, `internal/rabbitmq/rabbitmqtest/fake.go` (fakes shared by the producer and consumer tests), `internal/rabbitmq/topology_test.go`. Modify `internal/rabbitmq/topology.go`, `internal/producer/producer.go`; create `internal/producer/producer_test.go`.

**Interfaces — Produces:**

```go
// rabbitmq
type Channel interface {
	ExchangeDeclare(name, kind string, durable, autoDelete, internal, noWait bool, args amqp.Table) error
	QueueDeclare(name string, durable, autoDelete, exclusive, noWait bool, args amqp.Table) (amqp.Queue, error)
	QueueBind(name, key, exchange string, noWait bool, args amqp.Table) error
	Qos(prefetchCount, prefetchSize int, global bool) error
	Confirm(noWait bool) error
	NotifyPublish(confirm chan amqp.Confirmation) chan amqp.Confirmation
	Publish(exchange, key string, mandatory, immediate bool, msg amqp.Publishing) error
	Consume(queue, consumer string, autoAck, exclusive, noLocal, noWait bool, args amqp.Table) (<-chan amqp.Delivery, error)
	Cancel(consumer string, noWait bool) error
}
type Connection interface {
	Channel() (Channel, error)
	NotifyClose(receiver chan *amqp.Error) chan *amqp.Error
	IsClosed() bool
	Close() error
}
type Dialer func(url string) (Connection, error)
func Dial(url string) (Connection, error) // the real Dialer
func DeclareTopology(ch Channel) error

// rabbitmqtest
type Connection struct{ /* scriptable errors per method, records calls */ }
type Channel struct{ /* same; Published []amqp.Publishing; Ack/Nack next confirm */ }

// producer
func NewProducer(url string, dial rabbitmq.Dialer, logger *zap.Logger) (*RabbitMQProducer, error)
func (p *RabbitMQProducer) Ping(ctx context.Context) error // reconnects if the connection is gone, so an idle copy recovers
```

Make `startupConnectDelay`, `publishRetryDelay` and `confirmTimeout` fields with those defaults so tests set them to milliseconds.

- [ ] **Step 1: Tests**

```go
// topology_test.go
func TestDeclareTopologyDeclaresQueuePerNotificationType(t *testing.T) // queue_email, queue_sms, queue_push, dead_letter_queue
func TestDeclareTopologyDeadLettersNotificationQueues(t *testing.T)    // args["x-dead-letter-exchange"] == DeadLetterExchange
func TestDeclareTopologyStopsAtFirstFailure(t *testing.T)              // one row per failing call: exchange, queue, bind
// producer_test.go
func TestPublishReturnsAfterBrokerConfirms(t *testing.T)
func TestPublishSendsPersistentJSON(t *testing.T)
func TestPublishReconnectsAfterChannelFailure(t *testing.T)   // first Publish errors → dial called twice, second attempt succeeds
func TestPublishFailsWhenBrokerNacks(t *testing.T)
func TestPublishFailsWhenConfirmTimesOut(t *testing.T)
func TestPublishStopsRetryingWhenContextIsCancelled(t *testing.T)
func TestPublishGivesUpAfterThreeAttempts(t *testing.T)       // dial count == 3 reconnects, error wraps last cause
func TestNewProducerRetriesStartupConnection(t *testing.T)    // dial fails 2×, then ok
func TestNewProducerFailsAfterFiveAttempts(t *testing.T)
func TestPingReconnectsClosedConnection(t *testing.T)
func TestPingReportsUnreachableBroker(t *testing.T)
func TestCloseIsSafeToCallTwice(t *testing.T)
```

- [ ] **Step 2–4:** fail, implement, pass with `-race`.
- [ ] **Step 5:** Commit `refactor(rabbitmq): put AMQP behind interfaces; add producer Ping`.

---

### Task 9: Sweeper

**Files:** Create `internal/sweeper/sweeper.go`, `sweeper_test.go`.

**Interfaces:**
- Consumes: `repository.StaleNotification`, `ClaimStalePending`, `MarkQueued`, `UpdateNotificationStatus`; `Publisher.Publish`; `rabbitmq.NotificationExchange`; `metrics.Server`.
- Produces:

```go
type Store interface {
	ClaimStalePending(ctx context.Context, staleAfter time.Duration, limit int) ([]repository.StaleNotification, error)
	MarkQueued(ctx context.Context, id int64) error
	UpdateNotificationStatus(ctx context.Context, id int64, status string) error
}
type Publisher interface {
	Publish(ctx context.Context, exchange, routingKey string, body []byte) error
}
func New(store Store, publisher Publisher, cfg config.SweepConfig, m *metrics.Server, logger *zap.Logger) *Sweeper
func (s *Sweeper) SweepOnce(ctx context.Context)
func (s *Sweeper) Run(ctx context.Context) // SweepOnce every cfg.Interval until ctx is done
```

Per claimed row: if `SweepAttempts > cfg.MaxAttempts` → status `failed`, count `SweepFailures`; else publish → `MarkQueued`, count `SweepRepublished`; on publish error leave it `pending` (it is retried after the next stale window) and count `SweepFailures`. One row's failure never stops the pass.

- [ ] **Step 1: Tests**

```go
func TestSweepOnceRepublishesStaleNotification(t *testing.T)        // published routing key "push", body carries the id; status queued
func TestSweepOnceLeavesNotificationPendingWhenPublishFails(t *testing.T)
func TestSweepOnceFailsNotificationAfterMaxAttempts(t *testing.T)   // SweepAttempts 4, MaxAttempts 3 → failed, nothing published
func TestSweepOnceContinuesAfterOneFailure(t *testing.T)            // two rows, first publish fails → second still queued
func TestSweepOnceReportsClaimFailure(t *testing.T)                 // claim errors → no publish, one Error log
func TestSweepOncePassesConfiguredAgeAndBatch(t *testing.T)
func TestRunSweepsRepeatedlyUntilCancelled(t *testing.T)            // Interval 5ms; claim called ≥ 2; Run returns after cancel
```

- [ ] **Step 2–4:** fail, implement, pass. **Step 5:** Commit `feat(sweeper): re-publish notifications stuck in pending`.

---

### Task 10: ntfy sender — token, 429, header-safe title

**Files:** Modify `internal/service/ntfy.go`, `ntfy_test.go`.

**Interfaces — Produces:**

```go
const maxRetryAfter = time.Minute
type RetryAfterError struct{ Delay time.Duration } // Delay 0 means "server gave no usable hint"
func (e *RetryAfterError) Error() string
func NewNtfySender(cfg config.NtfyConfig) *NtfySender // uses BaseURL, Topic, Token, Timeout
func (s *NtfySender) Send(ctx context.Context, message notification.Message) error
```

On `429`: return `*RetryAfterError`; `Retry-After` parsed as integer seconds or an HTTP date, capped at `maxRetryAfter`, `0` if absent, unparseable or in the past. Token set → `Authorization: Bearer <token>`. Title is sent with `mime.QEncoding.Encode("utf-8", title)`, which leaves ASCII untouched.

- [ ] **Step 1: Tests** — port the three existing tests to the new constructor, then add:

```go
func TestSendAuthenticatesWithToken(t *testing.T)            // header "Authorization" == "Bearer tk_abc"
func TestSendOmitsAuthorizationWithoutToken(t *testing.T)
func TestSendEncodesNonASCIITitle(t *testing.T)              // title "Größe" → header decodes back to "Größe" via mime.WordDecoder
func TestSendOmitsPriorityHeaderWhenUnset(t *testing.T)
func TestSendReportsRetryAfterSecondsOnRateLimit(t *testing.T) // 429, "Retry-After: 7" → errors.As RetryAfterError, Delay == 7s
func TestSendCapsRetryAfter(t *testing.T)                    // "3600" → time.Minute
func TestSendReportsNoHintForUnparseableRetryAfter(t *testing.T) // rows: absent, "soon", "-5" → Delay == 0
func TestSendEscapesTopicInURL(t *testing.T)                 // topic "a b/c" → path "/a%20b%2Fc"
func TestSendStopsWhenContextIsCancelled(t *testing.T)
func TestSendReportsInvalidBaseURL(t *testing.T)             // BaseURL "://bad" → error, no panic
```

- [ ] **Step 2–4:** fail, implement, pass. **Step 5:** Commit `feat(ntfy): access token, rate-limit backoff hint and header-safe titles`.

---

### Task 11: Processor and consumer

**Files:** Modify `internal/consumer/processor.go`, `processor_test.go`, `consumer.go`; create `consumer_test.go`.

**Interfaces:**
- Consumes: `rabbitmq.Dialer`/`Connection`/`Channel` and `rabbitmqtest` fakes (Task 8), `service.RetryAfterError` (Task 10), `metrics.Worker` (Task 6), `repository.ErrNotificationNotFound`.
- Produces:

```go
type StatusStore interface {
	GetNotificationStatus(ctx context.Context, id int64) (string, error)
	UpdateNotificationStatus(ctx context.Context, id int64, status string) error
}
func NewProcessor(store StatusStore, senders map[string]Sender, retry RetryPolicy, m *metrics.Worker, logger *zap.Logger) *Processor

type Options struct {
	URL            string
	Workers        int
	ReconnectDelay time.Duration // default 5s
	Dial           rabbitmq.Dialer
}
func NewConsumer(opts Options, processor *Processor, m *metrics.Worker, logger *zap.Logger) *Consumer
func (c *Consumer) Run(ctx context.Context)
func (c *Consumer) Connected() bool
```

Processor order: decode → look up status → if settled, ack and count `skipped` → if row not found, dead-letter without sending → send with retries → record status → ack or dead-letter. A lookup error other than not-found does not block delivery: log it and send. Between attempts wait `RetryAfterError.Delay` when it is `> 0`, otherwise `retry.Delay`. Count `Sends{type,outcome}`, observe `SendDuration`, count `DeadLettered`.

Consumer: subscribes only to the three notification queues — **not** `dead_letter_queue` (delete `logDeadLetter`). Counts `Reconnects` on each reconnect. `Connected` is true between a successful subscribe and the connection closing.

- [ ] **Step 1: Tests** — port the six existing processor tests, then add:

```go
// processor_test.go
func TestProcessSkipsNotificationAlreadySent(t *testing.T)          // acked, sender.calls == 0
func TestProcessSkipsNotificationAlreadyFailed(t *testing.T)
func TestProcessDeadLettersNotificationMissingFromStore(t *testing.T) // dead-lettered, sender.calls == 0
func TestProcessSendsWhenStatusLookupFails(t *testing.T)            // lookup errors (not NotFound) → sent and acked
func TestProcessWaitsForRetryAfterHint(t *testing.T)                // sender returns RetryAfterError{30ms} once; elapsed ≥ 30ms with retry.Delay 0
func TestProcessUsesConfiguredDelayWithoutHint(t *testing.T)        // RetryAfterError{0}, retry.Delay 30ms → elapsed ≥ 30ms
func TestProcessStopsRetryingWhenContextEnds(t *testing.T)
func TestProcessCountsSendOutcomes(t *testing.T)                    // sent, failed, skipped each increment their label
func TestProcessCountsDeadLetters(t *testing.T)
func TestProcessLogsAckFailure(t *testing.T)                        // acknowledger.Ack errors → one Error log, no panic
func TestProcessLogsCarryNotificationID(t *testing.T)               // every entry logged after decoding has field notification_id == 7
// consumer_test.go (rabbitmqtest fakes)
func TestRunDeliversMessagesFromEveryNotificationQueue(t *testing.T)
func TestRunDoesNotConsumeDeadLetterQueue(t *testing.T)             // Consume never called with DeadLetterQueue
func TestRunSetsPrefetchToWorkerCount(t *testing.T)
func TestRunReconnectsAfterConnectionLoss(t *testing.T)             // close fake conn → dial called again; Reconnects == 1
func TestRunRetriesWhenDialFails(t *testing.T)
func TestRunRetriesWhenSetupFails(t *testing.T)                     // rows: Channel(), DeclareTopology, Qos, Consume each failing once
func TestRunFinishesInFlightMessageBeforeReturning(t *testing.T)    // cancel ctx mid-send → Run returns only after ack
func TestRunCancelsSubscriptionsOnShutdown(t *testing.T)            // Cancel called for each queue
func TestConnectedReflectsConnectionState(t *testing.T)
```

- [ ] **Step 2–4:** fail, implement, pass with `-race`. **Step 5:** Commit `feat(consumer): skip settled notifications, honour retry hints, keep dead letters`.

---

### Task 12: Health

**Files:** Create `internal/health/health.go`, `health_test.go`. Modify `internal/ratelimiter/ratelimiter.go` (+ test) to add `Ping`.

**Interfaces — Produces:**

```go
// ratelimiter
func (rl *RateLimiter) Ping(ctx context.Context) error

// health
type Check func(ctx context.Context) error
type Monitor struct{ /* unexported */ }
func NewMonitor(checks map[string]Check, interval time.Duration, logger *zap.Logger) *Monitor
func (m *Monitor) Run(ctx context.Context, onChange func(healthy bool)) // checks immediately, then every interval; calls onChange on first result and on every change
func (m *Monitor) Healthy() bool                                         // false until the first pass succeeds
func (m *Monitor) Handler() http.Handler                                 // 200 "ok" or 503 with the failing check names, one per line
```

Each check gets a context with timeout = `interval`. A failing check is logged once when it starts failing and once when it recovers, not every pass.

- [ ] **Step 1: Tests**

```go
func TestPingReportsUnreachableRedis(t *testing.T)           // miniredis.Close() → error   (ratelimiter)
func TestMonitorIsUnhealthyBeforeFirstCheck(t *testing.T)
func TestMonitorReportsHealthyWhenEveryCheckPasses(t *testing.T)
func TestMonitorReportsUnhealthyWhenAnyCheckFails(t *testing.T)
func TestMonitorNotifiesOnlyOnChange(t *testing.T)           // 5 passing passes → onChange called once (true)
func TestMonitorRecovers(t *testing.T)                       // fail then pass → onChange false, then true
func TestMonitorTimesOutHangingCheck(t *testing.T)           // check blocks on ctx → unhealthy, Run not stuck
func TestHandlerReturns503NamingFailedChecks(t *testing.T)   // body contains "redis", not "postgres"
func TestHandlerReturns200WhenHealthy(t *testing.T)
func TestMonitorLogsFailureOnce(t *testing.T)
```

- [ ] **Step 2–4:** fail, implement, pass. **Step 5:** Commit `feat(health): dependency health monitor`.

---

### Task 13: Process wiring and thin mains

**Files:** Create `internal/app/server.go`, `worker.go`, `app_test.go`. Rewrite `cmd/server/main.go`, `cmd/worker/main.go`.

**Interfaces — Produces:**

```go
func RunServer(ctx context.Context, cfg *config.Config, logger *zap.Logger) error
func RunWorker(ctx context.Context, cfg *config.Config, logger *zap.Logger) error
```

Each `main`: `config.Load("./config")` → `Validate*` → `logging.New` → `signal.NotifyContext(SIGINT, SIGTERM)` → `app.Run*` → non-zero exit on error. Nothing else.

`RunServer`: validate nothing itself; build Postgres, Redis limiter, producer, a fresh `prometheus.NewRegistry()` (plus Go and process collectors), `metrics.Server`, key ring (or `auth.AllowAll` when disabled), gRPC server with interceptors chained in the order **logging → metrics → auth** (so rejected calls are logged and counted), health service, reflection only if `cfg.GRPC.Reflection`. Start: metrics HTTP server (`/metrics` from the registry), health monitor (Postgres `Ping`, limiter `Ping`, producer `Ping`) whose `onChange` sets the gRPC health status for service `""`, sweeper. On `ctx.Done()`: set `NOT_SERVING`, `GracefulStop`, shut down the metrics server, close producer/limiter/DB. Returns the first startup error, or `nil` after a clean shutdown. Auth exempt methods: `/grpc.health.v1.Health/Check`, `/grpc.health.v1.Health/Watch`.

`RunWorker`: Postgres, `metrics.Worker` on a fresh registry, ntfy sender, processor, consumer; HTTP server on `cfg.Worker.MetricsPort` serving `/metrics` and `/healthz` (monitor checks: Postgres `Ping`, consumer `Connected`). On `ctx.Done()`: consumer drains, then HTTP server and DB close.

Split each `Run*` into small build/start/stop functions so no function exceeds ~20 lines.

- [ ] **Step 1: Unit tests for startup failures** (no services needed):

```go
func TestRunServerFailsWhenDatabaseIsUnreachable(t *testing.T)   // DSN on a closed port, ConnTimeout 100ms → ErrorContains "PostgresDB"
func TestRunServerFailsOnInvalidAPIKeys(t *testing.T)            // APIKeys "broken" → ErrorContains "auth.apiKeys"; the key ring is built before any connection, so no services are needed
func TestRunWorkerFailsWhenDatabaseIsUnreachable(t *testing.T)
```

The happy paths and shutdown are covered by Task 14.

- [ ] **Step 2–4:** fail, implement, pass; `go build ./...`.
- [ ] **Step 5:** `grep -rnE '"log"|println|fmt\.Print' --include='*.go' --exclude='*_test.go' cmd config internal` → no output.
- [ ] **Step 6:** Commit `refactor(app): wire processes in internal/app; reduce mains to entry points`.

---

### Task 14: Integration suite

**Files:** Create `deployments/docker-compose.test.yaml`, `internal/integration/harness_test.go`, `delivery_test.go`, `resilience_test.go`, `server_test.go`, `migrations_test.go`. Modify `Makefile`, `go.mod`.

**Compose (test):** `rabbitmq:3.13-management` (host ports `55672`→5672, `55673`→15672), `postgres:15` (`55432`, db/user/password `go_notify_test`), `redis:7-alpine` (`56379`); each with a health check; no volumes.

**Makefile:**

```make
test:                 ## unit tests
	go test -race ./...
test-services-up:
	docker compose -f deployments/docker-compose.test.yaml up -d --wait
test-services-down:
	docker compose -f deployments/docker-compose.test.yaml down -v
test-integration: test-services-up
	go test -race -tags=integration -count=1 ./internal/integration/...
```

**Harness** (all files start with `//go:build integration`; package `integration_test`). Addresses come from env with the compose defaults: `TEST_RABBITMQ_URL`, `TEST_RABBITMQ_API` (management URL), `TEST_POSTGRES_DSN`, `TEST_REDIS_ADDR`.

```go
func newStack(t *testing.T) *stack // per test: fresh schema via goose up, purged queues, flushed Redis, ntfy stub, config
func (s *stack) startServer(t *testing.T) pb.NotificationServiceClient // app.RunServer on free ports; waits for health SERVING; client sends x-api-key
func (s *stack) startWorker(t *testing.T)                              // app.RunWorker; waits for /healthz 200
func (s *stack) dropBrokerConnections(t *testing.T)                    // DELETE /api/connections/{name} via the management API
func (s *stack) eventuallyStatus(t *testing.T, id int64, want string)  // polls GetNotificationStatus, 15s timeout
type ntfyStub struct{ /* httptest.Server; records requests; scriptable status codes */ }
type tcpProxy struct{ /* in-test listener forwarding to real Redis; Stop() closes listener and live connections, Start() reopens on the same port */ }
// The server under test reaches Redis through s.redisProxy, so a test can make Redis unreachable without controlling Docker.
```

- [ ] **Step 1:** Write harness + compose; `make test-services-up` → three healthy containers.
- [ ] **Step 2: Write the tests**

```go
// delivery_test.go
func TestNotificationIsDeliveredAndMarkedSent(t *testing.T)        // stub got 1 request with title/body; status sent
func TestNotificationIsQueuedWhileWorkerIsDown(t *testing.T)       // no worker → status queued; start worker → sent
func TestUndeliverableNotificationIsDeadLettered(t *testing.T)     // stub always 500 → status failed; dead_letter_queue depth 1 (management API)
func TestRateLimitedPushIsRetriedAfterHint(t *testing.T)           // stub 429 Retry-After 1, then 200 → sent, 2 requests
// resilience_test.go
func TestServerPublishesAgainAfterBrokerConnectionDrop(t *testing.T)
func TestWorkerResumesAfterBrokerConnectionDrop(t *testing.T)      // drop, send, → sent; worker /metrics shows reconnects ≥ 1
func TestHealthReportsNotServingWhileRedisIsUnreachable(t *testing.T) // redisProxy.Stop() → health NOT_SERVING; Start() → SERVING again
func TestSweepRepublishesNotificationStuckInPending(t *testing.T)  // insert a pending row aged 10m directly; sweep interval 200ms → sent
func TestWorkerFinishesInFlightNotificationOnShutdown(t *testing.T) // stub delays 500ms; cancel worker ctx mid-send → status sent, message not redelivered
func TestServerStopsCleanly(t *testing.T)                          // cancel ctx → RunServer returns nil within 5s
// server_test.go
func TestRequestWithoutAPIKeyIsRejected(t *testing.T)              // Unauthenticated
func TestHealthCheckNeedsNoAPIKey(t *testing.T)
func TestRateLimitIsEnforcedAcrossRequests(t *testing.T)           // limit 2 → third ResourceExhausted
func TestReflectionIsOffByDefault(t *testing.T)                    // reflection client → Unimplemented
func TestMetricsEndpointExposesRequestCounter(t *testing.T)
// migrations_test.go
func TestMigrationsApplyAndRollBack(t *testing.T)                  // goose up, down to 0, up again → no error
func TestMigrationFailsLeftoverPendingRows(t *testing.T)           // up to first migration, insert pending + NULL-status rows, up → both failed
```

- [ ] **Step 3:** `make test-integration` → expect failures that reveal real defects in the never-exercised broker code. Fix each in the owning package with a unit test first where a fake can reproduce it; note every fix in the commit message.
- [ ] **Step 4:** `make test-integration` three times in a row → PASS each time (flakes are bugs; fix the cause, never add sleeps or retries to the test).
- [ ] **Step 5:** Commit `test: integration suite against RabbitMQ, Postgres and Redis`.

---

### Task 15: Coverage gate at 100%

**Files:** Create `scripts/check-coverage.sh`. Modify `Makefile`.

```make
COVER_PKGS := ./config/...,./internal/...
coverage: test-services-up
	go test -race -tags=integration -count=1 -coverpkg=$(COVER_PKGS) -coverprofile=coverage.out ./...
	./scripts/check-coverage.sh coverage.out
```

`check-coverage.sh <profile>`: runs `go tool cover -func`, prints every function below 100%, exits 1 unless the `total:` line is exactly `100.0%`. Test-support packages (`rabbitmqtest`) are excluded from the profile by filtering their lines before the check; add `coverage.out` to `.gitignore`.

- [ ] **Step 1:** Write the script; `make coverage` → FAIL, listing uncovered functions.
- [ ] **Step 2:** For each uncovered statement, in this order of preference: (a) add a behavioural test for it; (b) if no input can reach it, restructure or delete it. Record each (b) in the commit message.
- [ ] **Step 3:** `make coverage` → `total: 100.0%`, exit 0.
- [ ] **Step 4:** Commit `test: enforce 100% statement coverage`.

---

### Task 16: Containers

**Files:** Modify `deployments/docker/Dockerfile.grpc`, `Dockerfile.worker`, `deployments/docker-compose.yaml`, `deployments/prometheus/prometheus.yaml`. Create `.dockerignore`.

- Builder `golang:1.23.4-bookworm`; runtime `gcr.io/distroless/static-debian12:nonroot`; `CGO_ENABLED=0`, `-trimpath -ldflags="-s -w"`; build with `./cmd/server` / `./cmd/worker` (package path, not `main.go`).
- `grpc-health-probe` v0.4.37 downloaded for `TARGETARCH` and verified against a pinned SHA-256 per architecture (amd64, arm64); build fails on mismatch.
- Config comes from env only; the image contains just the binary (and the probe, for the server).
- Compose: image names `go-notify-server` / `go-notify-worker` via `image: ${DOCKER_USERNAME:-local}/go-notify-server:${IMAGE_TAG:-dev}`; `restart: unless-stopped` on both; health checks on Postgres (`pg_isready`) and Redis (`redis-cli ping`) with `depends_on: condition: service_healthy`; add Redis to the server's dependencies; `AUTH_APIKEYS=local-dev:change-me`; worker `WORKER_METRICSPORT=:9092` exposed and added as a Prometheus target; a one-shot `migrate` service running goose so `docker compose up` is self-contained; pin `prom/prometheus`, `grafana/grafana`, `grafana/loki` to exact current versions; remove unused `RABBITMQ_QUEUE`.

- [ ] **Step 1:** `docker compose -f deployments/docker-compose.yaml build` → both images build.
- [ ] **Step 2:** `docker compose -f deployments/docker-compose.yaml up -d --wait` → every service healthy.
- [ ] **Step 3:** Smoke test with `grpcurl` (install with `go install github.com/fullstorydev/grpcurl/cmd/grpcurl@v1.9.1`; set `GRPC_REFLECTION=true` in compose for local dev):

```bash
grpcurl -plaintext -H 'x-api-key: change-me' -d '{"user_id":"u1","title":"Hi","message":"hello","type":"push"}' localhost:50051 notify.NotificationService/SendNotification
```

Expected: `"success": true` and a `notificationId`; `GetNotificationStatus` for that id reaches `sent` (use a throwaway ntfy topic) and `curl -s localhost:9090/api/v1/targets` shows both targets `up`.
- [ ] **Step 4:** Verify non-root: `docker inspect -f '{{.Config.User}}' <image>` for both images → `nonroot` or `65532`.
- [ ] **Step 5:** `docker compose down -v`; commit `build: distroless non-root images and a self-contained compose stack`.

---

### Task 17: CI

**Files:** Create `.golangci.yaml`, `.github/workflows/ci.yaml`. Delete `.github/workflows/ci-cd.yaml`. Modify `Makefile` (`lint`, `proto`, `tidy-check`).

`.golangci.yaml`: enable `errcheck, govet, staticcheck, ineffassign, unused, errorlint, gocritic, revive, misspell, bodyclose, noctx, contextcheck, gocyclo (max 10), funlen (lines 40, statements 30), forbidigo` (forbid `^log\.`, `^fmt\.Print`, `^println$` outside `_test.go`); exclude `api/generated`.

Workflow: triggers `push` to `main` and `pull_request`; top-level `permissions: contents: read`; `concurrency` cancels superseded runs; every job has `timeout-minutes`; Go from `go-version-file: go.mod`; actions pinned to exact release tags. A reusable setup step installs `protoc` (pinned version via `arduino/setup-protoc`) and `protoc-gen-go@v1.36.1`, `protoc-gen-go-grpc@v1.5.1`, then `make proto`.

| Job | Does |
|---|---|
| `lint` | `gofmt -l .` empty; `go vet ./...`; `golangci-lint` (pinned action + version) |
| `generate` | `make proto`; `go mod tidy` then `git diff --exit-code go.mod go.sum` |
| `unit` | `make test` |
| `integration` | `make test-integration` (compose on the runner) |
| `coverage` | `make coverage`; upload `coverage.out` as an artifact |
| `security` | `govulncheck ./...` (pinned version) |
| `images` | `needs: [lint, generate, unit, integration, coverage, security]`; buildx build of both Dockerfiles; on `push` to `main` only: Docker Hub login and push with tags `${{ github.sha }}` and `latest` to `${{ secrets.DOCKER_USERNAME }}/go-notify-server` and `-worker` |

- [ ] **Step 1:** Install locally: `brew install golangci-lint actionlint`; `go install golang.org/x/vuln/cmd/govulncheck@latest`.
- [ ] **Step 2:** `golangci-lint run` → fix every finding in code (no blanket `nolint`; a justified one names the linter and the reason).
- [ ] **Step 3:** `actionlint .github/workflows/ci.yaml` → no output. `govulncheck ./...` → no vulnerabilities, or upgrade the affected dependency and note it.
- [ ] **Step 4:** Run every job's command locally: `make lint tidy-check test test-integration coverage` → all pass.
- [ ] **Step 5:** Commit `ci: split pipeline into lint, generate, unit, integration, coverage, security and images`. The workflow itself is unproven until a branch is pushed; say so in the commit body.

---

### Task 18: Documentation

**Files:** Rewrite `README.md`, `CONTRIBUTING.md`. Create `docs/architecture.md`, `docs/api.md`, `docs/configuration.md`, `docs/operations.md`, `docs/development.md`, `config/docs_test.go`, a `doc.go` in every package under `internal/` and in `config`. Modify `api/proto/notify.proto` (comments only; mark `success` and `error` `[deprecated = true]`).

Content each file must contain (wording is the writer's):

- **README.md** — one-paragraph description (internal gRPC service, push via ntfy); a Mermaid flow diagram of server → Postgres/RabbitMQ → worker → ntfy; quickstart: `make docker-up`, the `grpcurl` send and status calls from Task 16; links to the five docs. Remove every claim about email, SMS, WebSockets, Twilio, SendGrid and "REST/GraphQL integration".
- **docs/architecture.md** — components; life of a notification as a numbered sequence; the four statuses and every transition with what causes it; at-least-once guarantee and when duplicates can happen; failure table (Postgres, Redis, RabbitMQ, ntfy, worker down, server crash mid-request → what callers see, what recovers it).
- **docs/api.md** — both RPCs; every field with limits from Task 4; every status code each RPC can return and what the caller should do; `x-api-key`; deprecated fields; `grpcurl` and Go client examples.
- **docs/configuration.md** — one table row per key: key, env var, default, required-for (server/worker), meaning. Secrets called out (`auth.apiKeys`, `ntfy.token`, DSNs).
- **docs/operations.md** — load balancer setup (gRPC health check on service `""`, HTTP/2, TLS ends at the balancer); running migrations before deploy; scaling (stateless; any number of server and worker copies); metrics list with suggested alerts (dead letters > 0, sweep failures, 5xx-class codes, reconnects); log fields; inspecting and replaying `dead_letter_queue` with the RabbitMQ management UI and `rabbitmqadmin`; rotating an API key without downtime (add new, deploy, switch caller, remove old); shutdown behaviour and recommended termination grace period (≥ 60s for the worker); **privacy caveat: one shared ntfy topic, every subscriber sees every notification, the topic name is the only access control on public ntfy.sh**.
- **docs/development.md** — prerequisites and versions; `make` targets; proto generation; unit vs integration tests; the 100% coverage rule and how to satisfy it; project layout table; how to add a provider (implement `consumer.Sender`, register it in `RunWorker`, add the type to `notification.SupportedTypes`).
- **CONTRIBUTING.md** — correct build and test commands (the current ones do not exist); link to `docs/development.md`.

- [ ] **Step 1: Failing doc test** in `config/docs_test.go`:

```go
// Fails when a config key exists without a row in docs/configuration.md.
func TestEveryConfigKeyIsDocumented(t *testing.T) {
	documentation := readFile(t, "../docs/configuration.md")
	for _, key := range documentedKeys() { // every key Load reads, exposed for this test
		assert.Contains(t, documentation, "`"+key+"`")
	}
}
func TestExampleConfigListsEveryKey(t *testing.T) // every key appears in config.example.yaml
```

- [ ] **Step 2:** Run → FAIL (file missing). **Step 3:** Write all documents and `doc.go` files; comment the proto; `make proto`.
- [ ] **Step 4:** Verify claims against reality: run every command shown in README and docs against the compose stack and confirm the shown output; `go doc ./internal/...` prints a synopsis for every package; `make coverage` still 100%.
- [ ] **Step 5:** Commit `docs: architecture, API, configuration, operations and development guides`.

---

### Task 19: Final verification

- [ ] **Step 1:** From a clean tree: `make lint tidy-check test test-integration coverage` → all pass.
- [ ] **Step 2:** `make docker-up`, repeat the Task 16 smoke test, `make docker-down`.
- [ ] **Step 3:** Walk the spec section by section and tick each requirement against the code or docs that satisfy it; list anything unmet.
- [ ] **Step 4:** Run the Clean Code "Before submitting code" checklist over the diff `main..production-hardening`; fix or record each deviation.
- [ ] **Step 5:** Report to the owner: what was verified and how, what is unverified (the GitHub Actions workflow until pushed), every defect found by the integration suite, and every deviation from the spec. Do not push.
