//go:build integration

package integration_test

import (
	"net/http"
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/officiallysidsingh/go-notify/internal/notification"
	"github.com/officiallysidsingh/go-notify/internal/rabbitmq"
)

func TestServerPublishesAgainAfterBrokerConnectionDrop(t *testing.T) {
	s := newStack(t)
	s.startServer(t)
	s.startWorker(t)
	s.eventuallyStatus(t, s.send(t), notification.StatusSent)

	s.dropBrokerConnections(t)

	id := s.send(t)
	s.eventuallyStatus(t, id, notification.StatusSent)
}

func TestWorkerResumesAfterBrokerConnectionDrop(t *testing.T) {
	s := newStack(t)
	s.startServer(t)
	s.startWorker(t)
	s.eventuallyStatus(t, s.send(t), notification.StatusSent)

	s.dropBrokerConnections(t)
	id := s.send(t)

	s.eventuallyStatus(t, id, notification.StatusSent)
	match := regexp.MustCompile(`(?m)^gonotify_broker_reconnects_total (\d+)`).
		FindStringSubmatch(workerMetrics(t, s))
	require.NotNil(t, match, "reconnect counter missing from worker metrics")
	reconnects, err := strconv.Atoi(match[1])
	require.NoError(t, err)
	assert.GreaterOrEqual(t, reconnects, 1)
}

func TestHealthReportsNotServingWhileRedisIsUnreachable(t *testing.T) {
	s := newStack(t)
	s.startServer(t)

	healthIs := func(want grpc_health_v1.HealthCheckResponse_ServingStatus) func() bool {
		return func() bool {
			got, err := s.healthStatus(s.grpcConn)
			return err == nil && got == want
		}
	}

	s.redisProxy.Stop(t)
	require.Eventually(t, healthIs(grpc_health_v1.HealthCheckResponse_NOT_SERVING),
		10*time.Second, 50*time.Millisecond, "health never turned NOT_SERVING")

	s.redisProxy.Start(t)
	require.Eventually(t, healthIs(grpc_health_v1.HealthCheckResponse_SERVING),
		10*time.Second, 50*time.Millisecond, "health never recovered to SERVING")
}

func TestSweepRepublishesNotificationStuckInPending(t *testing.T) {
	s := newStack(t)
	s.cfg.Sweep.Interval = 200 * time.Millisecond
	var id int64
	err := s.db.QueryRow(`
		INSERT INTO notifications (user_id, title, type, priority, message, status, updated_at)
		VALUES ('user-1', 'stuck', 'push', '', 'was never published', 'pending', now() - interval '10 minutes')
		RETURNING id`).Scan(&id)
	require.NoError(t, err)

	s.startServer(t)
	s.startWorker(t)

	s.eventuallyStatus(t, id, notification.StatusSent)
	requests := s.ntfy.Requests()
	require.Len(t, requests, 1)
	assert.Equal(t, "was never published", requests[0].Body)
}

func TestWorkerFinishesInFlightNotificationOnShutdown(t *testing.T) {
	s := newStack(t)
	s.ntfy.SetHandler(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	})
	s.startServer(t)
	s.startWorker(t)
	id := s.send(t)
	require.Eventually(t, func() bool { return len(s.ntfy.Requests()) == 1 },
		10*time.Second, 10*time.Millisecond, "worker never started sending")

	s.cancelWorker()

	select {
	case <-s.workerDone:
	case <-time.After(10 * time.Second):
		t.Fatal("worker did not stop")
	}
	require.NoError(t, s.workerErr)
	assert.Equal(t, notification.StatusSent, s.status(t, id))
	assert.Zero(t, queueDepth(t, rabbitmq.QueueForType(notification.TypePush)),
		"message must be acknowledged, not left for redelivery")
	assert.Len(t, s.ntfy.Requests(), 1)
}

func TestServerStopsCleanly(t *testing.T) {
	s := newStack(t)
	s.startServer(t)
	s.eventuallyStatus(t, s.send(t), notification.StatusQueued)

	s.cancelServer()

	select {
	case <-s.serverDone:
	case <-time.After(5 * time.Second):
		t.Fatal("RunServer did not return within 5s")
	}
	assert.NoError(t, s.serverErr)
}
