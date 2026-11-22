package producer

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/streadway/amqp"
	"go.uber.org/zap"

	"github.com/officiallysidsingh/go-notify/internal/rabbitmq"
)

const (
	startupConnectAttempts = 5
	publishAttempts        = 3

	defaultStartupConnectDelay = 2 * time.Second
	defaultPublishRetryDelay   = time.Second
	defaultConfirmTimeout      = 5 * time.Second
)

var errNotConfirmed = errors.New("broker did not confirm the message")

// RabbitMQProducer publishes with broker confirms and reconnects after a
// failed publish, so a broker restart does not leave it permanently broken.
type RabbitMQProducer struct {
	url    string
	dial   rabbitmq.Dialer
	logger *zap.Logger

	startupConnectDelay time.Duration
	publishRetryDelay   time.Duration
	confirmTimeout      time.Duration

	// Confirms are matched to publishes by order, so publishing is serialized.
	mu       sync.Mutex
	conn     rabbitmq.Connection
	channel  rabbitmq.Channel
	confirms <-chan amqp.Confirmation
}

// NewProducer connects to the broker, retrying while it starts up.
func NewProducer(url string, dial rabbitmq.Dialer, logger *zap.Logger) (*RabbitMQProducer, error) {
	return newProducer(
		url, dial, logger,
		defaultStartupConnectDelay, defaultPublishRetryDelay, defaultConfirmTimeout,
	)
}

func newProducer(
	url string,
	dial rabbitmq.Dialer,
	logger *zap.Logger,
	startupDelay, retryDelay, confirmTimeout time.Duration,
) (*RabbitMQProducer, error) {
	if logger == nil {
		logger = zap.NewNop()
	}
	producer := &RabbitMQProducer{
		url:                 url,
		dial:                dial,
		logger:              logger,
		startupConnectDelay: startupDelay,
		publishRetryDelay:   retryDelay,
		confirmTimeout:      confirmTimeout,
	}

	var err error
	for attempt := 1; attempt <= startupConnectAttempts; attempt++ {
		if err = producer.connect(); err == nil {
			return producer, nil
		}
		logger.Warn("RabbitMQ connection failed, retrying",
			zap.Int("attempt", attempt),
			zap.Int("max_attempts", startupConnectAttempts),
			zap.Error(err),
		)
		if attempt < startupConnectAttempts {
			time.Sleep(startupDelay)
		}
	}
	return nil, fmt.Errorf("failed to connect to RabbitMQ: %w", err)
}

// Ping reconnects if the connection is gone, so an idle producer recovers
// without waiting for the next publish.
func (p *RabbitMQProducer) Ping(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.conn == nil || p.conn.IsClosed() {
		p.disconnect()
		if err := p.connect(); err != nil {
			return fmt.Errorf("rabbitmq unreachable: %w", err)
		}
	}
	return nil
}

func (p *RabbitMQProducer) connect() error {
	conn, err := p.dial(p.url)
	if err != nil {
		return fmt.Errorf("failed to dial: %w", err)
	}

	channel, confirms, err := p.openConfirmingChannel(conn)
	if err != nil {
		p.closeQuietly(conn)
		return err
	}

	p.conn, p.channel, p.confirms = conn, channel, confirms
	return nil
}

func (p *RabbitMQProducer) openConfirmingChannel(conn rabbitmq.Connection) (rabbitmq.Channel, <-chan amqp.Confirmation, error) {
	channel, err := conn.Channel()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open a channel: %w", err)
	}
	if err := rabbitmq.DeclareTopology(channel); err != nil {
		p.closeChannelQuietly(channel)
		return nil, nil, err
	}
	const noWait = false
	if err := channel.Confirm(noWait); err != nil {
		p.closeChannelQuietly(channel)
		return nil, nil, fmt.Errorf("failed to enable publisher confirms: %w", err)
	}
	return channel, channel.NotifyPublish(make(chan amqp.Confirmation, 1)), nil
}

// Publish delivers body to the exchange and returns once the broker has
// confirmed it. A message may be delivered more than once if a confirm is lost.
func (p *RabbitMQProducer) Publish(
	ctx context.Context,
	exchange, routingKey string,
	body []byte,
) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return fmt.Errorf("failed to publish message to %s: %w", exchange, err)
	}

	var err error
	for attempt := 1; attempt <= publishAttempts; attempt++ {
		if err = p.publishOnce(ctx, exchange, routingKey, body); err == nil {
			return nil
		}
		p.disconnect()
		p.logger.Warn("failed to publish message",
			zap.Int("attempt", attempt),
			zap.Int("max_attempts", publishAttempts),
			zap.Error(err),
		)
		if attempt == publishAttempts {
			break
		}
		if !waitOrDone(ctx, p.publishRetryDelay) {
			err = fmt.Errorf("%w (last error: %v)", ctx.Err(), err)
			break
		}
	}
	return fmt.Errorf("failed to publish message to %s: %w", exchange, err)
}

func (p *RabbitMQProducer) publishOnce(
	ctx context.Context,
	exchange, routingKey string,
	body []byte,
) error {
	if p.conn == nil || p.conn.IsClosed() {
		p.disconnect()
		if err := p.connect(); err != nil {
			return err
		}
	}

	const mandatory, immediate = false, false
	err := p.channel.Publish(exchange, routingKey, mandatory, immediate, amqp.Publishing{
		ContentType:  "application/json",
		Body:         body,
		DeliveryMode: amqp.Persistent,
	})
	if err != nil {
		return err
	}
	return p.awaitConfirm(ctx)
}

func (p *RabbitMQProducer) awaitConfirm(ctx context.Context) error {
	select {
	case confirmation, open := <-p.confirms:
		if !open || !confirmation.Ack {
			return errNotConfirmed
		}
		return nil
	case <-time.After(p.confirmTimeout):
		return errNotConfirmed
	case <-ctx.Done():
		return ctx.Err()
	}
}

// disconnect drops the connection so the next publish starts on a fresh one,
// with no stale confirms left over from a failed publish.
func (p *RabbitMQProducer) disconnect() {
	if p.conn != nil {
		p.closeQuietly(p.conn)
	}
	p.conn, p.channel, p.confirms = nil, nil, nil
}

func (p *RabbitMQProducer) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.disconnect()
}

func (p *RabbitMQProducer) closeQuietly(conn rabbitmq.Connection) {
	if err := conn.Close(); err != nil && !errors.Is(err, amqp.ErrClosed) {
		p.logger.Error("error closing producer connection", zap.Error(err))
	}
}

func (p *RabbitMQProducer) closeChannelQuietly(ch rabbitmq.Channel) {
	if err := ch.Close(); err != nil && !errors.Is(err, amqp.ErrClosed) {
		p.logger.Error("error closing producer channel", zap.Error(err))
	}
}

func waitOrDone(ctx context.Context, delay time.Duration) bool {
	select {
	case <-time.After(delay):
		return true
	case <-ctx.Done():
		return false
	}
}
