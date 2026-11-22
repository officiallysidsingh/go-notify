package consumer

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/streadway/amqp"
	"go.uber.org/zap"

	"github.com/officiallysidsingh/go-notify/internal/metrics"
	"github.com/officiallysidsingh/go-notify/internal/notification"
	"github.com/officiallysidsingh/go-notify/internal/rabbitmq"
)

const (
	defaultReconnectDelay = 5 * time.Second
	defaultJobTimeout     = time.Minute
)

// ErrSessionAborted is the cancellation cause of in-flight work when the
// broker connection is lost: the channel is gone, so deliveries stay unsettled
// and the broker redelivers them. Any other context ending is a job timeout.
var ErrSessionAborted = errors.New("consumer session aborted")

// Options configures a Consumer.
type Options struct {
	URL            string
	Workers        int
	ReconnectDelay time.Duration // defaults to 5s
	Dial           rabbitmq.Dialer
	JobTimeout     time.Duration // per-delivery processing limit; defaults to 1m
}

// Consumer feeds deliveries from every notification queue to a shared worker
// pool and reconnects whenever the broker connection is lost. The dead-letter
// queue is deliberately not consumed: its messages are kept for inspection.
type Consumer struct {
	opts      Options
	processor *Processor
	metrics   *metrics.Worker
	logger    *zap.Logger
	connected atomic.Bool
}

func NewConsumer(opts Options, processor *Processor, m *metrics.Worker, logger *zap.Logger) *Consumer {
	if opts.ReconnectDelay <= 0 {
		opts.ReconnectDelay = defaultReconnectDelay
	}
	if opts.Workers < 1 {
		opts.Workers = 1
	}
	if opts.JobTimeout <= 0 {
		opts.JobTimeout = defaultJobTimeout
	}
	if opts.Dial == nil {
		opts.Dial = rabbitmq.Dial
	}
	return &Consumer{opts: opts, processor: processor, metrics: m, logger: logger}
}

// Connected reports whether the consumer is subscribed on a live connection.
func (c *Consumer) Connected() bool {
	return c.connected.Load()
}

// Run consumes until ctx is cancelled, then returns once in-flight
// notifications have been settled.
func (c *Consumer) Run(ctx context.Context) {
	for ctx.Err() == nil {
		if err := c.runSession(ctx); err != nil {
			c.logger.Warn("consumer session failed", zap.Error(err), zap.Duration("retry_in", c.opts.ReconnectDelay))
		}
		if ctx.Err() != nil {
			return
		}
		waitOrDone(ctx, c.opts.ReconnectDelay)
	}
}

// runSession consumes over one connection until ctx ends or the connection is lost.
func (c *Consumer) runSession(ctx context.Context) error {
	conn, err := c.opts.Dial(c.opts.URL)
	if err != nil {
		return fmt.Errorf("failed to dial: %w", err)
	}
	defer c.closeConnection(conn)
	connectionLost := conn.NotifyClose(make(chan *amqp.Error, 1))

	subscription, err := c.subscribe(conn)
	if err != nil {
		return err
	}
	workers := c.startWorkers(ctx, subscription.streams)
	c.connected.Store(true)
	c.logger.Info("consumer is up and running, waiting for messages")

	select {
	case <-ctx.Done():
		c.cancelSubscriptions(subscription)
		c.connected.Store(false)
		workers.shutdown(false)
		return nil
	case lost := <-connectionLost:
		c.connected.Store(false)
		c.metrics.Reconnects.Inc()
		workers.shutdown(true)
		return fmt.Errorf("lost broker connection: %w", lostCause(lost))
	}
}

// errConnectionClosed stands in for the cause when the broker closes the
// connection without sending an error.
var errConnectionClosed = errors.New("connection closed")

func lostCause(lost *amqp.Error) error {
	if lost == nil {
		return errConnectionClosed
	}
	return *lost
}

type subscription struct {
	channel rabbitmq.Channel
	tags    []string
	streams []<-chan amqp.Delivery
}

// subscribe opens a channel, declares the topology and consumes every
// notification queue. Each queue is consumed under its own name as the
// consumer tag, which is what cancelSubscriptions cancels.
func (c *Consumer) subscribe(conn rabbitmq.Connection) (*subscription, error) {
	channel, err := conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("failed to open a channel: %w", err)
	}
	if err := rabbitmq.DeclareTopology(channel); err != nil {
		return nil, err
	}
	const prefetchSize, global = 0, false
	if err := channel.Qos(c.opts.Workers, prefetchSize, global); err != nil {
		return nil, fmt.Errorf("failed to set prefetch: %w", err)
	}

	sub := &subscription{channel: channel}
	for _, notificationType := range notification.Types() {
		queue := rabbitmq.QueueForType(notificationType)
		deliveries, err := consume(channel, queue)
		if err != nil {
			return nil, err
		}
		sub.tags = append(sub.tags, queue)
		sub.streams = append(sub.streams, deliveries)
	}
	return sub, nil
}

func consume(channel rabbitmq.Channel, queue string) (<-chan amqp.Delivery, error) {
	const autoAck, exclusive, noLocal, noWait = false, false, false, false
	deliveries, err := channel.Consume(queue, queue, autoAck, exclusive, noLocal, noWait, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to consume from %s: %w", queue, err)
	}
	return deliveries, nil
}

// cancelSubscriptions stops new deliveries while keeping the channel open, so
// in-flight notifications can still be acknowledged.
func (c *Consumer) cancelSubscriptions(sub *subscription) {
	const noWait = false
	for _, tag := range sub.tags {
		if err := sub.channel.Cancel(tag, noWait); err != nil {
			c.logger.Warn("failed to cancel consumer", zap.String("queue", tag), zap.Error(err))
		}
	}
}

func (c *Consumer) closeConnection(conn rabbitmq.Connection) {
	if err := conn.Close(); err != nil && !errors.Is(err, amqp.ErrClosed) {
		c.logger.Warn("failed to close consumer connection", zap.Error(err))
	}
}

// workerPool merges the queue streams into one and runs workers over it.
type workerPool struct {
	merged     chan amqp.Delivery
	stop       chan struct{}
	forwarders sync.WaitGroup
	workers    sync.WaitGroup
	sessionCtx context.Context
	cancelWork context.CancelCauseFunc
}

func (c *Consumer) startWorkers(ctx context.Context, streams []<-chan amqp.Delivery) *workerPool {
	// In-flight work outlives shutdown on purpose: a notification is finished
	// and settled rather than abandoned half-sent. Only losing the connection
	// cancels the session; each delivery also gets its own timeout.
	sessionCtx, cancel := context.WithCancelCause(context.WithoutCancel(ctx))
	pool := &workerPool{
		merged:     make(chan amqp.Delivery),
		stop:       make(chan struct{}),
		sessionCtx: sessionCtx,
		cancelWork: cancel,
	}
	for _, stream := range streams {
		pool.forwarders.Add(1)
		go pool.forward(stream)
	}
	for range c.opts.Workers {
		pool.workers.Add(1)
		go c.work(sessionCtx, pool)
	}
	return pool
}

// work processes merged deliveries until the pool closes them.
func (c *Consumer) work(ctx context.Context, pool *workerPool) {
	defer pool.workers.Done()
	for delivery := range pool.merged {
		jobCtx, cancelJob := context.WithTimeout(ctx, c.opts.JobTimeout)
		c.processor.Process(jobCtx, delivery)
		cancelJob()
	}
}

func (p *workerPool) forward(stream <-chan amqp.Delivery) {
	defer p.forwarders.Done()
	for {
		select {
		case <-p.stop:
			return
		case delivery, open := <-stream:
			if !open {
				return
			}
			select {
			case p.merged <- delivery:
			case <-p.stop:
				return
			}
		}
	}
}

// shutdown stops intake and waits for workers to finish what they hold. With
// abort set, in-flight work is told to stop too (the connection is gone, so
// it could not be settled anyway).
func (p *workerPool) shutdown(abort bool) {
	close(p.stop)
	p.forwarders.Wait()
	close(p.merged)
	if abort {
		p.cancelWork(ErrSessionAborted)
	}
	p.workers.Wait()
	p.cancelWork(context.Canceled)
}
