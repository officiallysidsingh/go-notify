package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/streadway/amqp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/officiallysidsingh/go-notify/internal/notification"
)

const (
	testNotificationID = int64(7)
	testMaxAttempts    = 3
)

type fakeAcknowledger struct {
	acked    bool
	nacked   bool
	requeued bool
}

func (a *fakeAcknowledger) Ack(uint64, bool) error {
	a.acked = true
	return nil
}

func (a *fakeAcknowledger) Nack(_ uint64, _ bool, requeue bool) error {
	a.nacked = true
	a.requeued = requeue
	return nil
}

func (a *fakeAcknowledger) Reject(uint64, bool) error { return nil }

func (a *fakeAcknowledger) deadLettered() bool {
	return a.nacked && !a.requeued
}

type fakeSender struct {
	failures int
	calls    int
}

func (s *fakeSender) Send(context.Context, notification.Message) error {
	s.calls++
	if s.calls <= s.failures {
		return errors.New("provider unavailable")
	}
	return nil
}

type fakeStatusStore struct {
	err      error
	statuses map[int64]string
}

func (s *fakeStatusStore) UpdateNotificationStatus(_ context.Context, id int64, status string) error {
	if s.err != nil {
		return s.err
	}
	s.statuses[id] = status
	return nil
}

type processorFixture struct {
	processor    *Processor
	sender       *fakeSender
	store        *fakeStatusStore
	acknowledger *fakeAcknowledger
}

func newProcessorFixture() *processorFixture {
	fixture := &processorFixture{
		sender:       &fakeSender{},
		store:        &fakeStatusStore{statuses: map[int64]string{}},
		acknowledger: &fakeAcknowledger{},
	}
	senders := map[string]Sender{notification.TypePush: fixture.sender}
	fixture.processor = NewProcessor(fixture.store, senders, RetryPolicy{MaxAttempts: testMaxAttempts})
	return fixture
}

func (f *processorFixture) deliveryOfType(t *testing.T, notificationType string) amqp.Delivery {
	t.Helper()
	body, err := json.Marshal(notification.Message{
		NotificationID: testNotificationID,
		Type:           notificationType,
	})
	require.NoError(t, err)
	return amqp.Delivery{Acknowledger: f.acknowledger, Body: body}
}

func TestProcessAcknowledgesDeliveredNotification(t *testing.T) {
	fixture := newProcessorFixture()

	fixture.processor.Process(context.Background(), fixture.deliveryOfType(t, notification.TypePush))

	assert.True(t, fixture.acknowledger.acked)
	assert.Equal(t, notification.StatusSent, fixture.store.statuses[testNotificationID])
}

func TestProcessRetriesTransientSendFailure(t *testing.T) {
	fixture := newProcessorFixture()
	fixture.sender.failures = testMaxAttempts - 1

	fixture.processor.Process(context.Background(), fixture.deliveryOfType(t, notification.TypePush))

	assert.True(t, fixture.acknowledger.acked)
	assert.Equal(t, notification.StatusSent, fixture.store.statuses[testNotificationID])
}

func TestProcessDeadLettersNotificationAfterExhaustingAttempts(t *testing.T) {
	fixture := newProcessorFixture()
	fixture.sender.failures = testMaxAttempts

	fixture.processor.Process(context.Background(), fixture.deliveryOfType(t, notification.TypePush))

	assert.True(t, fixture.acknowledger.deadLettered())
	assert.Equal(t, testMaxAttempts, fixture.sender.calls)
	assert.Equal(t, notification.StatusFailed, fixture.store.statuses[testNotificationID])
}

func TestProcessFailsNotificationForChannelWithoutSender(t *testing.T) {
	fixture := newProcessorFixture()

	fixture.processor.Process(context.Background(), fixture.deliveryOfType(t, notification.TypeEmail))

	assert.True(t, fixture.acknowledger.deadLettered())
	assert.Equal(t, notification.StatusFailed, fixture.store.statuses[testNotificationID])
}

func TestProcessDoesNotResendWhenStatusUpdateFails(t *testing.T) {
	fixture := newProcessorFixture()
	fixture.store.err = errors.New("database unavailable")

	fixture.processor.Process(context.Background(), fixture.deliveryOfType(t, notification.TypePush))

	assert.True(t, fixture.acknowledger.acked)
	assert.Equal(t, 1, fixture.sender.calls)
}

func TestProcessDeadLettersMalformedMessage(t *testing.T) {
	fixture := newProcessorFixture()
	delivery := amqp.Delivery{Acknowledger: fixture.acknowledger, Body: []byte("not json")}

	fixture.processor.Process(context.Background(), delivery)

	assert.True(t, fixture.acknowledger.deadLettered())
	assert.Zero(t, fixture.sender.calls)
}
