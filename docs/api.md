# API

The service exposes one gRPC service, `notify.NotificationService`, defined in
[`api/proto/notify.proto`](../api/proto/notify.proto). Generated Go code lives
in `api/generated/`.

## Authentication

Every call must carry an `x-api-key` metadata header. The key must match an
entry in `auth.apiKeys` (`name:key` pairs); the name is logged as the caller.
The standard `grpc.health.v1.Health` checks are exempt. Calls without a valid
key get `Unauthenticated`.

## SendNotification

`SendNotification(NotificationRequest) -> NotificationResponse`

Stores the notification and queues it for delivery.

| Field | Type | Rules |
|---|---|---|
| `user_id` | string | required, at most 255 bytes, no control characters; also the rate-limit key |
| `title` | string | optional, at most 250 bytes, no control characters |
| `priority` | string | `""` or `"1"` to `"5"` (ntfy priorities) |
| `message` | string | required, at most 4096 bytes |
| `type` | string | `"push"` (the only supported type) |

Response: `notification_id` (int64) is the ID to use with
`GetNotificationStatus`. `success` and `error` are deprecated.

| Code | Meaning | Caller should |
|---|---|---|
| `OK` | accepted and queued | store the ID |
| `InvalidArgument` | validation failed | fix the request; do not retry |
| `Unauthenticated` | missing or unknown key | fix credentials |
| `ResourceExhausted` | rate limit hit | back off and retry later |
| `Unavailable` | a dependency (Postgres, Redis, RabbitMQ) is down | retry with backoff |
| `Internal` | storage error | retry; report if persistent |

## GetNotificationStatus

`GetNotificationStatus(StatusRequest) -> StatusResponse`

| Field | Type | Rules |
|---|---|---|
| `notification_id` | int64 | must be positive |

Response: `status` is one of `pending`, `queued`, `sent`, `failed`. The `error`
field is deprecated.

| Code | Meaning |
|---|---|
| `OK` | status returned |
| `InvalidArgument` | ID is zero or negative |
| `Unauthenticated` | missing or unknown key |
| `NotFound` | no notification with that ID |
| `Internal` | storage error |

## Deprecated fields

`success` and `error` in `NotificationResponse`, and `error` in
`StatusResponse`, are deprecated and kept only for wire compatibility. Clients
should check the gRPC status code instead.

## Examples

grpcurl:

```bash
grpcurl -plaintext -H 'x-api-key: change-me' \
  -d '{"user_id":"u1","title":"Hi","message":"hello","type":"push"}' \
  localhost:50051 notify.NotificationService/SendNotification
```

Go:

```go
conn, err := grpc.NewClient("localhost:50051",
    grpc.WithTransportCredentials(insecure.NewCredentials()))
if err != nil {
    log.Fatal(err)
}
defer conn.Close()

client := pb.NewNotificationServiceClient(conn)
ctx := metadata.AppendToOutgoingContext(context.Background(), "x-api-key", "change-me")

resp, err := client.SendNotification(ctx, &pb.NotificationRequest{
    UserId: "u1", Title: "Hi", Message: "hello", Type: "push",
})
if err != nil {
    log.Fatal(status.Code(err), err)
}
fmt.Println(resp.NotificationId)
```

Use TLS credentials instead of `insecure` outside local development.
