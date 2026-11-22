//go:build integration

package integration_test

import (
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/officiallysidsingh/go-notify/internal/notification"
	"github.com/officiallysidsingh/go-notify/internal/rabbitmq"
)

func TestNotificationIsDeliveredAndMarkedSent(t *testing.T) {
	s := newStack(t)
	s.startServer(t)
	s.startWorker(t)

	id := s.send(t)

	s.eventuallyStatus(t, id, notification.StatusSent)
	requests := s.ntfy.Requests()
	require.Len(t, requests, 1)
	assert.Equal(t, "hello", requests[0].Title)
	assert.Equal(t, "world", requests[0].Body)
	assert.Equal(t, "/test", requests[0].Path)
}

func TestNotificationIsQueuedWhileWorkerIsDown(t *testing.T) {
	s := newStack(t)
	s.startServer(t)

	id := s.send(t)
	assert.Equal(t, notification.StatusQueued, s.status(t, id))
	assert.Empty(t, s.ntfy.Requests())

	s.startWorker(t)
	s.eventuallyStatus(t, id, notification.StatusSent)
	assert.Len(t, s.ntfy.Requests(), 1)
}

func TestUndeliverableNotificationIsDeadLettered(t *testing.T) {
	s := newStack(t)
	s.ntfy.SetHandler(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	s.startServer(t)
	s.startWorker(t)

	id := s.send(t)

	s.eventuallyStatus(t, id, notification.StatusFailed)
	require.Eventually(t, func() bool {
		var queue struct {
			MessagesReady int `json:"messages_ready"`
		}
		if err := mgmtJSON("/queues/%2F/"+rabbitmq.DeadLetterQueue, &queue); err != nil {
			return false
		}
		return queue.MessagesReady == 1
	}, 20*time.Second, 250*time.Millisecond, "dead_letter_queue never held the message")
	assert.Len(t, s.ntfy.Requests(), s.cfg.Worker.SendAttempts)
}

func TestRateLimitedPushIsRetriedAfterHint(t *testing.T) {
	s := newStack(t)
	var calls atomic.Int32
	s.ntfy.SetHandler(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
		}
	})
	s.startServer(t)
	s.startWorker(t)

	id := s.send(t)

	s.eventuallyStatus(t, id, notification.StatusSent)
	requests := s.ntfy.Requests()
	require.Len(t, requests, 2)
	assert.GreaterOrEqual(t, requests[1].At.Sub(requests[0].At), 900*time.Millisecond,
		"retry should wait for the Retry-After hint, not the 10ms retry delay")
}
