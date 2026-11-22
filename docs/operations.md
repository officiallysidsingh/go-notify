# Operations

## Load balancer

- Use a gRPC health check against the standard `grpc.health.v1.Health/Check`
  with service name `""`. It reports `SERVING` only while Postgres, RabbitMQ
  and Redis checks pass.
- The listener must speak HTTP/2.
- TLS ends at the balancer; the server listens in plaintext.
- The worker exposes `/metrics` and `/healthz` over HTTP on `worker.metricsPort`.

## Migrations

Run `goose up` (or the migrate service in the compose stack) against the
database before deploying a new version. `make migrate-up` does this locally.

## Scaling

Both processes are stateless. Run any number of server and worker copies; the
sweeper claims rows in the database so concurrent servers do not double-publish.

## Metrics

Server (`metrics.port`) and worker (`worker.metricsPort`) serve Prometheus metrics at `/metrics`.

| Metric | Meaning | Suggested alert |
|---|---|---|
| `notifications_received_total` | notifications accepted | drop to zero when traffic is expected |
| `gonotify_grpc_requests_total{method,caller,code}` | requests by outcome | rate of `Internal`/`Unavailable` above baseline |
| `gonotify_grpc_request_duration_seconds` | request latency | p99 above your SLO |
| `gonotify_rate_limited_total` | rate-limited requests | sustained increase |
| `gonotify_sweep_republished_total` | stale rows republished | sustained increase (publish path is unhealthy) |
| `gonotify_sweep_failures_total` | failed sweep republishes | any increase |
| `gonotify_sends_total{type,outcome}` | send attempts | `failed` outcome rate |
| `gonotify_send_duration_seconds` | ntfy latency | p99 above timeout |
| `gonotify_dead_lettered_total` | messages dead-lettered | greater than 0 |
| `gonotify_broker_reconnects_total` | worker broker reconnects | repeated increases |

## Logs

Logs are JSON (zap). Each gRPC request logs one line with `method`, `caller`,
`code` and `duration`; request bodies are never logged. Worker and sweeper log
lines carry `notification_id`.

## Dead letters

Messages that fail permanently are routed to `dead_letter_queue`. Inspect them
in the RabbitMQ management UI (port 15672). To replay, fetch and republish
with `rabbitmqadmin`, for example:

```bash
rabbitmqadmin get queue=dead_letter_queue count=10 ackmode=ack_requeue_true
rabbitmqadmin publish exchange=notification_exchange_topic routing_key=push payload='<body>'
```

Fix the cause (for example ntfy credentials) before replaying; the row for a
dead-lettered message is already `failed`, so a replay is skipped by the
worker's settled check unless you reset the row's status first.

## Rotating an API key

1. Add the new `name:key` entry to `auth.apiKeys` alongside the old one.
2. Deploy.
3. Switch the caller to the new key.
4. Remove the old entry and deploy again.

## Shutdown

The server uses `GracefulStop`: it stops accepting new calls and lets
in-flight ones finish. The worker stops consuming and drains in-flight sends.
Set `terminationGracePeriodSeconds: 60` (or equivalent) so neither is killed
mid-send. Unacked messages are redelivered by the broker.

## Privacy caveat

All notifications are published to **one shared ntfy topic** (`ntfy.topic`).
Every subscriber to that topic sees every notification. On public ntfy.sh the
topic name is the only access control, so use a long random name, or run a
private ntfy server with authentication and set `ntfy.token`. Do not send
sensitive content.
