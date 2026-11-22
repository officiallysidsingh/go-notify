# Architecture

## Components

| Component | Role |
|---|---|
| gRPC server | Validates and authenticates requests, rate limits, stores the notification, publishes it to the broker, and serves status lookups. Also runs the sweeper. |
| Worker | Consumes messages from RabbitMQ, sends them to ntfy with retries, and records the final status. |
| Postgres | System of record for notifications and their status. |
| RabbitMQ | Durable queue between server and worker; its dead-letter queue holds messages that could not be delivered. |
| Redis | Per-user rate-limit counters. The server fails closed when Redis is unreachable. |
| ntfy | The push delivery provider. The only supported notification type is `push`. |

## Life of a notification

1. A backend calls `SendNotification` with an `x-api-key`.
2. The auth interceptor resolves the key to a caller name.
3. The request is validated (see [API](api.md)).
4. The per-user rate limit is checked in Redis.
5. The server inserts a row with status `pending` and gets its ID.
6. The server publishes the message to RabbitMQ and waits for the broker's publisher confirm.
7. The server marks the row `queued` and returns the ID to the caller.
8. The worker receives the message and checks the row; an already settled row is acked without sending.
9. The worker posts to ntfy (retrying up to `worker.sendAttempts`) and records `sent`, or `failed` and dead-letters the message.
10. The worker acks the message.

## Statuses

`pending`, `queued`, `sent`, `failed`.

| Transition | Cause |
|---|---|
| `pending` -> `queued` | server received broker confirmation |
| `queued` -> `sent` | worker pushed to ntfy successfully |
| `queued` -> `failed` | worker exhausted retries, or the type is unsupported |
| `pending` -> `failed` | enqueue failed; publisher/broker error |
| `pending` -> `failed` (sweep) | row exceeded `sweep.maxAttempts` |

`sent` and `failed` are terminal.

## Delivery guarantee

Delivery is **at-least-once**. Duplicates can happen if:

- `MarkQueued` fails after the publish succeeded: the row stays `pending`, and
  the sweeper republishes it after `sweep.staleAfter`.
- The status update fails after a successful send: the message is redelivered,
  and the worker's `IsSettled` check absorbs it when the row is already
  `sent` or `failed`. If the update itself failed, the row is not settled and a
  duplicate push is possible.

## Failure modes

| Failure | Caller sees | Recovery |
|---|---|---|
| Postgres down | `Unavailable` at insert time | Reconnect on next request |
| Redis down | `Unavailable` (fails closed) | Reconnect on next request; health flips |
| RabbitMQ down | `Unavailable` at publish | Row stays `pending`; sweep republishes once broker returns |
| ntfy error | row goes to `failed` -> dead_letter_queue after retries | Manual inspection via management API |
| Worker crash mid-send | message unacked -> redelivered on reconnect | Broker redelivery; `IsSettled` check avoids double-send |
| Server crash mid-request | row stays `pending` | Sweep republishes after StaleAfter |
