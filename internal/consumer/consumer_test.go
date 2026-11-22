package consumer

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/streadway/amqp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/officiallysidsingh/go-notify/internal/metrics"
	"github.com/officiallysidsingh/go-notify/internal/notification"
	"github.com/officiallysidsingh/go-notify/internal/rabbitmq"
	"github.com/officiallysidsingh/go-notify/internal/rabbitmq/rabbitmqtest"
)

const (
	testWorkers        = 2
	testReconnectDelay = 5 * time.Millisecond
	waitFor            = 2 * time.Second
	tick               = time.Millisecond
)

// brokerSession is one fake connection whose channel hands out deliveries
// from queues the test controls.
type brokerSession struct {
	conn       *rabbitmqtest.Connection
	channel    *rabbitmqtest.Channel
	deliveries map[string]chan amqp.Delivery
}

func newBrokerSession() *brokerSession {
	session := &brokerSession{
		conn:       rabbitmqtest.NewConnection(),
		channel:    rabbitmqtest.NewChannel(),
		deliveries: map[string]chan amqp.Delivery{},
	}
	for _, notificationType := range notification.Types() {
		queue := rabbitmq.QueueForType(notificationType)
		session.deliveries[queue] = make(chan amqp.Delivery, 1)
		session.channel.ConsumeDeliveries[queue] = session.deliveries[queue]
	}
	session.conn.ChannelFunc = func(int) (*rabbitmqtest.Channel, error) { return session.channel, nil }
	return session
}

func (s *brokerSession) enqueue(t *testing.T, notificationType string, acknowledger amqp.Acknowledger, id int64) {
	t.Helper()
	s.deliveries[rabbitmq.QueueForType(notificationType)] <- deliveryFor(t, acknowledger, id, notificationType)
}

type consumerFixture struct {
	consumer *Consumer
	dialer   *rabbitmqtest.Dialer
	senders  map[string]*fakeSender
	store    *fakeStatusStore
	metrics  *metrics.Worker
	running  *runningConsumer
}

func newConsumerFixture(dialer *rabbitmqtest.Dialer) *consumerFixture {
	return newConsumerFixtureWithTimeout(dialer, 0)
}

func newConsumerFixtureWithTimeout(dialer *rabbitmqtest.Dialer, jobTimeout time.Duration) *consumerFixture {
	fixture := &consumerFixture{
		dialer:  dialer,
		senders: map[string]*fakeSender{},
		store:   &fakeStatusStore{statuses: map[int64]string{}},
		metrics: metrics.NewWorker(prometheus.NewRegistry()),
	}
	senders := map[string]Sender{}
	for _, notificationType := range notification.Types() {
		fixture.senders[notificationType] = &fakeSender{}
		senders[notificationType] = fixture.senders[notificationType]
	}
	processor := NewProcessor(fixture.store, senders, RetryPolicy{MaxAttempts: 1}, fixture.metrics, zap.NewNop())
	fixture.consumer = NewConsumer(Options{
		URL:            "amqp://test",
		Workers:        testWorkers,
		ReconnectDelay: testReconnectDelay,
		Dial:           dialer.Dialer(),
		JobTimeout:     jobTimeout,
	}, processor, fixture.metrics, zap.NewNop())
	return fixture
}

type runningConsumer struct {
	cancel context.CancelFunc
	done   chan struct{}
}

func (f *consumerFixture) start(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	running := &runningConsumer{cancel: cancel, done: make(chan struct{})}
	f.running = running
	go func() {
		defer close(running.done)
		f.consumer.Run(ctx)
	}()
	t.Cleanup(func() { f.stop(t) })
}

// stop cancels the consumer and waits for Run to return.
func (f *consumerFixture) stop(t *testing.T) {
	t.Helper()
	f.running.cancel()
	select {
	case <-f.running.done:
	case <-time.After(waitFor):
		t.Fatal("Run did not return after cancellation")
	}
}

func (f *consumerFixture) waitConnected(t *testing.T) {
	t.Helper()
	require.Eventually(t, f.consumer.Connected, waitFor, tick)
}

func TestRunDeliversMessagesFromEveryNotificationQueue(t *testing.T) {
	session := newBrokerSession()
	fixture := newConsumerFixture(rabbitmqtest.NewDialer(session.conn))
	fixture.start(t)
	fixture.waitConnected(t)

	acknowledger := &fakeAcknowledger{}
	for _, notificationType := range notification.Types() {
		session.enqueue(t, notificationType, acknowledger, testNotificationID)
	}

	for notificationType, sender := range fixture.senders {
		assert.Eventually(t, func() bool { return sender.callCount() == 1 }, waitFor, tick, notificationType)
	}
}

func TestRunDoesNotConsumeDeadLetterQueue(t *testing.T) {
	session := newBrokerSession()
	fixture := newConsumerFixture(rabbitmqtest.NewDialer(session.conn))
	fixture.start(t)
	fixture.waitConnected(t)
	fixture.stop(t)

	var tags []string
	for tag := range session.channel.Consumers {
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	var queues []string
	for _, notificationType := range notification.Types() {
		queues = append(queues, rabbitmq.QueueForType(notificationType))
	}
	sort.Strings(queues)
	assert.Equal(t, queues, tags)
	assert.NotContains(t, tags, rabbitmq.DeadLetterQueue)
}

func TestRunSetsPrefetchToWorkerCount(t *testing.T) {
	session := newBrokerSession()
	fixture := newConsumerFixture(rabbitmqtest.NewDialer(session.conn))
	fixture.start(t)
	fixture.waitConnected(t)
	fixture.stop(t)

	assert.Equal(t, testWorkers, session.channel.QosPrefetchCount)
}

func TestRunReconnectsAfterConnectionLoss(t *testing.T) {
	first, second := newBrokerSession(), newBrokerSession()
	fixture := newConsumerFixture(rabbitmqtest.NewDialer(first.conn, second.conn))
	fixture.start(t)
	fixture.waitConnected(t)

	first.conn.FireClose(&amqp.Error{Code: amqp.ConnectionForced, Reason: "broker restart"})

	require.Eventually(t, func() bool { return fixture.dialer.DialCount() == 2 }, waitFor, tick)
	fixture.waitConnected(t)
	assert.Equal(t, 1.0, testutil.ToFloat64(fixture.metrics.Reconnects))

	acknowledger := &fakeAcknowledger{}
	second.enqueue(t, notification.TypePush, acknowledger, testNotificationID)
	assert.Eventually(t, acknowledger.acked, waitFor, tick)
}

func TestRunRetriesWhenDialFails(t *testing.T) {
	session := newBrokerSession()
	dialer := rabbitmqtest.NewScriptedDialer(
		rabbitmqtest.DialResult{Err: errors.New("broker down")},
		rabbitmqtest.DialResult{Conn: session.conn},
	)
	fixture := newConsumerFixture(dialer)
	fixture.start(t)

	fixture.waitConnected(t)
	assert.GreaterOrEqual(t, dialer.DialCount(), 2)
}

func TestRunRetriesWhenSetupFails(t *testing.T) {
	failOnce := func(call int) error {
		if call == 1 {
			return errors.New("scripted failure")
		}
		return nil
	}
	tests := map[string]func(*brokerSession){
		"Channel":         func(s *brokerSession) { s.conn.ChannelFunc = failFirstChannel(s) },
		"DeclareTopology": func(s *brokerSession) { s.channel.ExchangeDeclareErr = failOnce },
		"Qos":             func(s *brokerSession) { s.channel.QosErr = failOnce },
		"Consume":         func(s *brokerSession) { s.channel.ConsumeErr = failOnce },
	}
	for name, breakSetup := range tests {
		t.Run(name, func(t *testing.T) {
			session := newBrokerSession()
			breakSetup(session)
			fixture := newConsumerFixture(rabbitmqtest.NewDialer(session.conn))
			fixture.start(t)

			fixture.waitConnected(t)
			assert.GreaterOrEqual(t, fixture.dialer.DialCount(), 2)
			assert.GreaterOrEqual(t, session.conn.CloseCalls, 1)
		})
	}
}

func failFirstChannel(s *brokerSession) func(int) (*rabbitmqtest.Channel, error) {
	return func(call int) (*rabbitmqtest.Channel, error) {
		if call == 1 {
			return nil, errors.New("scripted failure")
		}
		return s.channel, nil
	}
}

func TestRunFinishesInFlightMessageBeforeReturning(t *testing.T) {
	session := newBrokerSession()
	fixture := newConsumerFixture(rabbitmqtest.NewDialer(session.conn))
	push := fixture.senders[notification.TypePush]
	push.hold = 100 * time.Millisecond
	push.started = make(chan struct{})
	fixture.start(t)
	fixture.waitConnected(t)

	acknowledger := &fakeAcknowledger{}
	session.enqueue(t, notification.TypePush, acknowledger, testNotificationID)
	<-push.started
	fixture.stop(t)

	assert.True(t, acknowledger.acked())
}

func TestRunCancelsSubscriptionsOnShutdown(t *testing.T) {
	session := newBrokerSession()
	fixture := newConsumerFixture(rabbitmqtest.NewDialer(session.conn))
	fixture.start(t)
	fixture.waitConnected(t)
	fixture.stop(t)

	var subscribed []string
	for tag := range session.channel.Consumers {
		subscribed = append(subscribed, tag)
	}
	assert.ElementsMatch(t, subscribed, session.channel.Cancelled)
	assert.Len(t, session.channel.Cancelled, len(notification.Types()))
	assert.GreaterOrEqual(t, session.conn.CloseCalls, 1)
}

func TestConnectedReflectsConnectionState(t *testing.T) {
	first := newBrokerSession()
	// The second dial never succeeds, so the disconnected state is stable.
	dialer := rabbitmqtest.NewScriptedDialer(
		rabbitmqtest.DialResult{Conn: first.conn},
		rabbitmqtest.DialResult{Err: errors.New("broker down")},
	)
	fixture := newConsumerFixture(dialer)
	assert.False(t, fixture.consumer.Connected())

	fixture.start(t)
	fixture.waitConnected(t)

	first.conn.FireClose(&amqp.Error{Reason: "gone"})
	assert.Eventually(t, func() bool { return !fixture.consumer.Connected() }, waitFor, tick)
}

// Regression: one context shared by every job expired after the job timeout
// and left all later deliveries unsettled.
func TestRunProcessesMessagesAfterJobTimeoutHasElapsed(t *testing.T) {
	const jobTimeout = 50 * time.Millisecond
	session := newBrokerSession()
	fixture := newConsumerFixtureWithTimeout(rabbitmqtest.NewDialer(session.conn), jobTimeout)
	fixture.senders[notification.TypePush].honourContext = true
	fixture.start(t)
	fixture.waitConnected(t)

	first, second := &fakeAcknowledger{}, &fakeAcknowledger{}
	session.enqueue(t, notification.TypePush, first, testNotificationID)
	require.Eventually(t, first.acked, waitFor, tick)
	time.Sleep(2 * jobTimeout)
	session.enqueue(t, notification.TypePush, second, testNotificationID+1)

	assert.Eventually(t, second.acked, waitFor, tick)
}

func TestNewConsumerAppliesDefaults(t *testing.T) {
	c := NewConsumer(Options{}, nil, nil, zap.NewNop())

	assert.Equal(t, defaultReconnectDelay, c.opts.ReconnectDelay)
	assert.Equal(t, 1, c.opts.Workers)
	assert.Equal(t, defaultJobTimeout, c.opts.JobTimeout)
	assert.NotNil(t, c.opts.Dial)
}

func TestRunWarnsWhenCancellingSubscriptionFails(t *testing.T) {
	core, logs := observer.New(zap.WarnLevel)
	session := newBrokerSession()
	session.channel.CancelErr = func(int) error { return errors.New("cancel failed") }
	fixture := newConsumerFixture(rabbitmqtest.NewDialer(session.conn))
	fixture.consumer.logger = zap.New(core)
	fixture.start(t)
	fixture.waitConnected(t)

	fixture.stop(t)

	assert.Equal(t, len(notification.Types()), logs.FilterMessage("failed to cancel consumer").Len())
}

func TestRunWarnsWhenClosingConnectionFails(t *testing.T) {
	core, logs := observer.New(zap.WarnLevel)
	session := newBrokerSession()
	session.conn.CloseErr = errors.New("close failed")
	fixture := newConsumerFixture(rabbitmqtest.NewDialer(session.conn))
	fixture.consumer.logger = zap.New(core)
	fixture.start(t)
	fixture.waitConnected(t)

	fixture.stop(t)

	assert.Equal(t, 1, logs.FilterMessage("failed to close consumer connection").Len())
}

func TestRunIgnoresAlreadyClosedConnectionOnShutdown(t *testing.T) {
	core, logs := observer.New(zap.WarnLevel)
	session := newBrokerSession()
	session.conn.CloseErr = amqp.ErrClosed
	fixture := newConsumerFixture(rabbitmqtest.NewDialer(session.conn))
	fixture.consumer.logger = zap.New(core)
	fixture.start(t)
	fixture.waitConnected(t)

	fixture.stop(t)

	assert.Equal(t, 1, session.conn.CloseCalls)
	assert.Zero(t, logs.Len())
}

func TestWorkerPoolStopsForwardingWhenStreamCloses(t *testing.T) {
	fixture := newConsumerFixture(rabbitmqtest.NewDialer(newBrokerSession().conn))
	stream := make(chan amqp.Delivery)
	close(stream)

	pool := fixture.consumer.startWorkers([]<-chan amqp.Delivery{stream})

	forwarded := make(chan struct{})
	go func() {
		pool.forwarders.Wait()
		close(forwarded)
	}()
	select {
	case <-forwarded:
	case <-time.After(waitFor):
		t.Fatal("forwarder kept running after its stream closed")
	}
	pool.shutdown(false)
}

func TestForwarderStopsWhileWaitingForFreeWorker(t *testing.T) {
	pool := &workerPool{merged: make(chan amqp.Delivery), stop: make(chan struct{})}
	stream := make(chan amqp.Delivery, 1)
	stream <- amqp.Delivery{Body: []byte("held")}
	pool.forwarders.Add(1)
	go pool.forward(stream)
	// Once the forwarder has taken the delivery it is blocked handing it over.
	require.Eventually(t, func() bool { return len(stream) == 0 }, waitFor, tick)

	close(pool.stop)

	forwarded := make(chan struct{})
	go func() {
		pool.forwarders.Wait()
		close(forwarded)
	}()
	select {
	case <-forwarded:
	case <-time.After(waitFor):
		t.Fatal("forwarder kept waiting for a worker after stop")
	}
	select {
	case delivery := <-pool.merged:
		t.Fatalf("unexpected delivery forwarded: %s", delivery.Body)
	default:
	}
}
