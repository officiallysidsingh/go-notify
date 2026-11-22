package producer

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/streadway/amqp"

	"github.com/officiallysidsingh/go-notify/internal/rabbitmq"
)

const (
	startupConnectAttempts = 5
	startupConnectDelay    = 2 * time.Second
	publishAttempts        = 3
	publishRetryDelay      = time.Second
	confirmTimeout         = 5 * time.Second
)

var errNotConfirmed = errors.New("broker did not confirm the message")

// RabbitMQProducer publishes with broker confirms and reconnects after a
// failed publish, so a broker restart does not leave it permanently broken.
type RabbitMQProducer struct {
	url string

	// Confirms are matched to publishes by order, so publishing is serialized.
	mu       sync.Mutex
	conn     *amqp.Connection
	channel  *amqp.Channel
	confirms <-chan amqp.Confirmation
}

func NewProducer(url string) (*RabbitMQProducer, error) {
	producer := &RabbitMQProducer{url: url}

	var err error
	for attempt := 1; attempt <= startupConnectAttempts; attempt++ {
		if err = producer.connect(); err == nil {
			return producer, nil
		}
		log.Printf(
			"RabbitMQ connection failed. Retrying... (%d/%d): %v",
			attempt, startupConnectAttempts, err,
		)
		time.Sleep(startupConnectDelay)
	}
	return nil, fmt.Errorf("failed to connect to RabbitMQ: %w", err)
}

func (p *RabbitMQProducer) connect() error {
	conn, err := amqp.Dial(p.url)
	if err != nil {
		return fmt.Errorf("failed to dial: %w", err)
	}

	channel, confirms, err := openConfirmingChannel(conn)
	if err != nil {
		closeQuietly(conn)
		return err
	}

	p.conn, p.channel, p.confirms = conn, channel, confirms
	return nil
}

func openConfirmingChannel(conn *amqp.Connection) (*amqp.Channel, <-chan amqp.Confirmation, error) {
	channel, err := conn.Channel()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open a channel: %w", err)
	}
	if err := rabbitmq.DeclareTopology(channel); err != nil {
		return nil, nil, err
	}
	const noWait = false
	if err := channel.Confirm(noWait); err != nil {
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

	var err error
	for attempt := 1; attempt <= publishAttempts; attempt++ {
		if err = p.publishOnce(ctx, exchange, routingKey, body); err == nil {
			return nil
		}
		p.disconnect()
		log.Printf("Failed to publish message (%d/%d): %v", attempt, publishAttempts, err)
		if attempt == publishAttempts || !waitOrDone(ctx, publishRetryDelay) {
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
	if p.conn == nil {
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
	case <-time.After(confirmTimeout):
		return errNotConfirmed
	case <-ctx.Done():
		return ctx.Err()
	}
}

// disconnect drops the connection so the next publish starts on a fresh one,
// with no stale confirms left over from a failed publish.
func (p *RabbitMQProducer) disconnect() {
	if p.conn != nil {
		closeQuietly(p.conn)
	}
	p.conn, p.channel, p.confirms = nil, nil, nil
}

func (p *RabbitMQProducer) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.disconnect()
}

func closeQuietly(conn *amqp.Connection) {
	if err := conn.Close(); err != nil && !errors.Is(err, amqp.ErrClosed) {
		log.Printf("error closing producer connection: %v", err)
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
