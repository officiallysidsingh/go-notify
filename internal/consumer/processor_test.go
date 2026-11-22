package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/streadway/amqp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/officiallysidsingh/go-notify/internal/metrics"
	"github.com/officiallysidsingh/go-notify/internal/notification"
	"github.com/officiallysidsingh/go-notify/internal/repository"
	"github.com/officiallysidsingh/go-notify/internal/service"
)

const (
	testNotificationID = int64(7)
	testMaxAttempts    = 3
	testHint           = 30 * time.Millisecond
)

type fakeAcknowledger struct {
	mu       sync.Mutex
	ackErr   error
	acks     int
	nacks    int
	requeued bool
}

func (a *fakeAcknowledger) Ack(uint64, bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.acks++
	return a.ackErr
}

func (a *fakeAcknowledger) Nack(_ uint64, _ bool, requeue bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.nacks++
	a.requeued = requeue
	return nil
}

func (a *fakeAcknowledger) Reject(uint64, bool) error { return nil }

func (a *fakeAcknowledger) acked() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.acks > 0
}

func (a *fakeAcknowledger) deadLettered() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.nacks > 0 && !a.requeued
}

func (a *fakeAcknowledger) settled() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.acks > 0 || a.nacks > 0
}

var errProvider = errors.New("provider unavailable")

// fakeSender fails the first `failures` calls with failWith (errProvider when
// nil). hold keeps each call in flight; started is closed on the first call.
type fakeSender struct {
	mu       sync.Mutex
	failures int
	failWith error
	hold     time.Duration
	calls    int
	started  chan struct{}
}

func (s *fakeSender) Send(context.Context, notification.Message) error {
	s.mu.Lock()
	s.calls++
	call := s.calls
	if s.started != nil && call == 1 {
		close(s.started)
	}
	s.mu.Unlock()

	time.Sleep(s.hold)
	if call <= s.failures {
		if s.failWith != nil {
			return s.failWith
		}
		return errProvider
	}
	return nil
}

func (s *fakeSender) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// fakeStatusStore answers lookups with lookupStatus (queued when empty) or
// lookupErr, and records status updates.
type fakeStatusStore struct {
	mu           sync.Mutex
	updateErr    error
	lookupErr    error
	lookupStatus string
	statuses     map[int64]string
}

func (s *fakeStatusStore) GetNotificationStatus(context.Context, int64) (string, error) {
	if s.lookupErr != nil {
		return "", s.lookupErr
	}
	if s.lookupStatus == "" {
		return notification.StatusQueued, nil
	}
	return s.lookupStatus, nil
}

func (s *fakeStatusStore) UpdateNotificationStatus(_ context.Context, id int64, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.updateErr != nil {
		return s.updateErr
	}
	s.statuses[id] = status
	return nil
}

func (s *fakeStatusStore) statusOf(id int64) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.statuses[id]
}

type processorFixture struct {
	processor    *Processor
	sender       *fakeSender
	store        *fakeStatusStore
	acknowledger *fakeAcknowledger
	metrics      *metrics.Worker
	logs         *observer.ObservedLogs
}

func newProcessorFixture(retryDelay time.Duration) *processorFixture {
	core, logs := observer.New(zapcore.DebugLevel)
	fixture := &processorFixture{
		sender:       &fakeSender{},
		store:        &fakeStatusStore{statuses: map[int64]string{}},
		acknowledger: &fakeAcknowledger{},
		metrics:      metrics.NewWorker(prometheus.NewRegistry()),
		logs:         logs,
	}
	senders := map[string]Sender{notification.TypePush: fixture.sender}
	retry := RetryPolicy{MaxAttempts: testMaxAttempts, Delay: retryDelay}
	fixture.processor = NewProcessor(fixture.store, senders, retry, fixture.metrics, zap.New(core))
	return fixture
}

func (f *processorFixture) deliveryOfType(t *testing.T, notificationType string) amqp.Delivery {
	t.Helper()
	return deliveryFor(t, f.acknowledger, testNotificationID, notificationType)
}

func deliveryFor(t *testing.T, acknowledger amqp.Acknowledger, id int64, notificationType string) amqp.Delivery {
	t.Helper()
	body, err := json.Marshal(notification.Message{NotificationID: id, Type: notificationType})
	require.NoError(t, err)
	return amqp.Delivery{Acknowledger: acknowledger, Body: body}
}

func (f *processorFixture) sends(notificationType, outcome string) float64 {
	return testutil.ToFloat64(f.metrics.Sends.WithLabelValues(notificationType, outcome))
}

func TestProcessAcknowledgesDeliveredNotification(t *testing.T) {
	fixture := newProcessorFixture(0)

	fixture.processor.Process(context.Background(), fixture.deliveryOfType(t, notification.TypePush))

	assert.True(t, fixture.acknowledger.acked())
	assert.Equal(t, notification.StatusSent, fixture.store.statusOf(testNotificationID))
}

func TestProcessRetriesTransientSendFailure(t *testing.T) {
	fixture := newProcessorFixture(0)
	fixture.sender.failures = testMaxAttempts - 1

	fixture.processor.Process(context.Background(), fixture.deliveryOfType(t, notification.TypePush))

	assert.True(t, fixture.acknowledger.acked())
	assert.Equal(t, notification.StatusSent, fixture.store.statusOf(testNotificationID))
}

func TestProcessDeadLettersNotificationAfterExhaustingAttempts(t *testing.T) {
	fixture := newProcessorFixture(0)
	fixture.sender.failures = testMaxAttempts

	fixture.processor.Process(context.Background(), fixture.deliveryOfType(t, notification.TypePush))

	assert.True(t, fixture.acknowledger.deadLettered())
	assert.Equal(t, testMaxAttempts, fixture.sender.callCount())
	assert.Equal(t, notification.StatusFailed, fixture.store.statusOf(testNotificationID))
}

func TestProcessFailsNotificationForChannelWithoutSender(t *testing.T) {
	fixture := newProcessorFixture(0)

	fixture.processor.Process(context.Background(), fixture.deliveryOfType(t, notification.TypeEmail))

	assert.True(t, fixture.acknowledger.deadLettered())
	assert.Equal(t, notification.StatusFailed, fixture.store.statusOf(testNotificationID))
}

func TestProcessDoesNotResendWhenStatusUpdateFails(t *testing.T) {
	fixture := newProcessorFixture(0)
	fixture.store.updateErr = errors.New("database unavailable")

	fixture.processor.Process(context.Background(), fixture.deliveryOfType(t, notification.TypePush))

	assert.True(t, fixture.acknowledger.acked())
	assert.Equal(t, 1, fixture.sender.callCount())
}

func TestProcessDeadLettersMalformedMessage(t *testing.T) {
	fixture := newProcessorFixture(0)
	delivery := amqp.Delivery{Acknowledger: fixture.acknowledger, Body: []byte("not json")}

	fixture.processor.Process(context.Background(), delivery)

	assert.True(t, fixture.acknowledger.deadLettered())
	assert.Zero(t, fixture.sender.callCount())
}

func TestProcessSkipsNotificationAlreadySent(t *testing.T) {
	fixture := newProcessorFixture(0)
	fixture.store.lookupStatus = notification.StatusSent

	fixture.processor.Process(context.Background(), fixture.deliveryOfType(t, notification.TypePush))

	assert.True(t, fixture.acknowledger.acked())
	assert.Zero(t, fixture.sender.callCount())
	assert.Equal(t, 1.0, fixture.sends(notification.TypePush, "skipped"))
}

func TestProcessSkipsNotificationAlreadyFailed(t *testing.T) {
	fixture := newProcessorFixture(0)
	fixture.store.lookupStatus = notification.StatusFailed

	fixture.processor.Process(context.Background(), fixture.deliveryOfType(t, notification.TypePush))

	assert.True(t, fixture.acknowledger.acked())
	assert.Zero(t, fixture.sender.callCount())
	assert.Equal(t, 1.0, fixture.sends(notification.TypePush, "skipped"))
}

func TestProcessDeadLettersNotificationMissingFromStore(t *testing.T) {
	fixture := newProcessorFixture(0)
	fixture.store.lookupErr = repository.ErrNotificationNotFound

	fixture.processor.Process(context.Background(), fixture.deliveryOfType(t, notification.TypePush))

	assert.True(t, fixture.acknowledger.deadLettered())
	assert.Zero(t, fixture.sender.callCount())
}

func TestProcessSendsWhenStatusLookupFails(t *testing.T) {
	fixture := newProcessorFixture(0)
	fixture.store.lookupErr = errors.New("database unavailable")

	fixture.processor.Process(context.Background(), fixture.deliveryOfType(t, notification.TypePush))

	assert.Equal(t, 1, fixture.sender.callCount())
	assert.True(t, fixture.acknowledger.acked())
	assert.Equal(t, 1, fixture.logs.FilterLevelExact(zapcore.WarnLevel).Len())
}

func TestProcessWaitsForRetryAfterHint(t *testing.T) {
	fixture := newProcessorFixture(0)
	fixture.sender.failures = 1
	fixture.sender.failWith = &service.RetryAfterError{Delay: testHint}

	start := time.Now()
	fixture.processor.Process(context.Background(), fixture.deliveryOfType(t, notification.TypePush))

	assert.GreaterOrEqual(t, time.Since(start), testHint)
	assert.True(t, fixture.acknowledger.acked())
}

func TestProcessUsesConfiguredDelayWithoutHint(t *testing.T) {
	fixture := newProcessorFixture(testHint)
	fixture.sender.failures = 1
	fixture.sender.failWith = &service.RetryAfterError{Delay: 0}

	start := time.Now()
	fixture.processor.Process(context.Background(), fixture.deliveryOfType(t, notification.TypePush))

	assert.GreaterOrEqual(t, time.Since(start), testHint)
	assert.True(t, fixture.acknowledger.acked())
}

func TestProcessStopsRetryingWhenContextEnds(t *testing.T) {
	fixture := newProcessorFixture(time.Hour)
	fixture.sender.failures = testMaxAttempts
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		defer close(done)
		fixture.processor.Process(ctx, fixture.deliveryOfType(t, notification.TypePush))
	}()
	require.Eventually(t, func() bool { return fixture.sender.callCount() == 1 }, 2*time.Second, time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Process kept waiting after the context ended")
	}
	assert.False(t, fixture.acknowledger.settled())
	assert.Equal(t, 1, fixture.sender.callCount())
}

func TestProcessCountsSendOutcomes(t *testing.T) {
	t.Run("sent", func(t *testing.T) {
		fixture := newProcessorFixture(0)
		fixture.processor.Process(context.Background(), fixture.deliveryOfType(t, notification.TypePush))
		assert.Equal(t, 1.0, fixture.sends(notification.TypePush, "sent"))
		assert.Zero(t, fixture.sends(notification.TypePush, "failed"))
		assert.Zero(t, fixture.sends(notification.TypePush, "skipped"))
	})
	t.Run("failed", func(t *testing.T) {
		fixture := newProcessorFixture(0)
		fixture.sender.failures = testMaxAttempts
		fixture.processor.Process(context.Background(), fixture.deliveryOfType(t, notification.TypePush))
		assert.Equal(t, 1.0, fixture.sends(notification.TypePush, "failed"))
		assert.Zero(t, fixture.sends(notification.TypePush, "sent"))
	})
	t.Run("skipped", func(t *testing.T) {
		fixture := newProcessorFixture(0)
		fixture.store.lookupStatus = notification.StatusSent
		fixture.processor.Process(context.Background(), fixture.deliveryOfType(t, notification.TypePush))
		assert.Equal(t, 1.0, fixture.sends(notification.TypePush, "skipped"))
		assert.Zero(t, fixture.sends(notification.TypePush, "sent"))
	})
}

func TestProcessObservesSendDuration(t *testing.T) {
	fixture := newProcessorFixture(0)
	fixture.sender.failures = 1

	fixture.processor.Process(context.Background(), fixture.deliveryOfType(t, notification.TypePush))

	assert.Equal(t, 1, testutil.CollectAndCount(fixture.metrics.SendDuration))
}

func TestProcessCountsDeadLetters(t *testing.T) {
	fixture := newProcessorFixture(0)
	fixture.sender.failures = testMaxAttempts

	fixture.processor.Process(context.Background(), fixture.deliveryOfType(t, notification.TypePush))

	assert.Equal(t, 1.0, testutil.ToFloat64(fixture.metrics.DeadLettered))
}

func TestProcessLogsAckFailure(t *testing.T) {
	fixture := newProcessorFixture(0)
	fixture.acknowledger.ackErr = errors.New("channel closed")

	assert.NotPanics(t, func() {
		fixture.processor.Process(context.Background(), fixture.deliveryOfType(t, notification.TypePush))
	})

	assert.Equal(t, 1, fixture.logs.FilterLevelExact(zapcore.ErrorLevel).Len())
}

func TestProcessLogsCarryNotificationID(t *testing.T) {
	fixture := newProcessorFixture(0)
	fixture.store.lookupErr = errors.New("database unavailable")
	fixture.sender.failures = testMaxAttempts
	fixture.acknowledger.ackErr = errors.New("channel closed")

	fixture.processor.Process(context.Background(), fixture.deliveryOfType(t, notification.TypePush))

	entries := fixture.logs.All()
	require.NotEmpty(t, entries)
	for _, entry := range entries {
		assert.Equal(t, testNotificationID, entry.ContextMap()["notification_id"], entry.Message)
	}
}
