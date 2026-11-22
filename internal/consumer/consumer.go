package consumer

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/streadway/amqp"

	"github.com/officiallysidsingh/go-notify/internal/notification"
	"github.com/officiallysidsingh/go-notify/internal/rabbitmq"
)

const (
	reconnectDelay    = 5 * time.Second
	processingTimeout = time.Minute
)

type deliveryHandler func(ctx context.Context, delivery amqp.Delivery)

type job struct {
	handle   deliveryHandler
	delivery amqp.Delivery
}

// Consumer feeds deliveries from every notification queue to a shared worker
// pool and reconnects whenever the broker connection is lost.
type Consumer struct {
	amqpURL   string
	workers   int
	processor *Processor
}

func NewConsumer(amqpURL string, workers int, processor *Processor) *Consumer {
	return &Consumer{amqpURL: amqpURL, workers: workers, processor: processor}
}

// Run consumes until ctx is cancelled, then returns once in-flight
// notifications have been settled.
func (c *Consumer) Run(ctx context.Context) {
	for ctx.Err() == nil {
		err := c.consumeUntilDisconnected(ctx)
		if ctx.Err() != nil {
			return
		}
		log.Printf("Lost RabbitMQ connection, reconnecting in %s: %v", reconnectDelay, err)
		waitOrDone(ctx, reconnectDelay)
	}
}

func (c *Consumer) consumeUntilDisconnected(ctx context.Context) error {
	conn, err := amqp.Dial(c.amqpURL)
	if err != nil {
		return fmt.Errorf("failed to dial: %w", err)
	}
	defer closeQuietly(conn)

	channel, err := c.openChannel(conn)
	if err != nil {
		return err
	}
	handlers := c.handlersByQueue()
	jobs, err := subscribe(channel, handlers)
	if err != nil {
		return err
	}
	log.Println("Consumer is up and running, waiting for messages...")

	workersDone := c.startWorkers(jobs)
	select {
	case <-ctx.Done():
		unsubscribe(channel, handlers)
	case err = <-conn.NotifyClose(make(chan *amqp.Error, 1)):
	}
	<-workersDone
	return err
}

func (c *Consumer) openChannel(conn *amqp.Connection) (*amqp.Channel, error) {
	channel, err := conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("failed to open a channel: %w", err)
	}
	if err := rabbitmq.DeclareTopology(channel); err != nil {
		return nil, err
	}
	const prefetchSize, global = 0, false
	if err := channel.Qos(c.workers, prefetchSize, global); err != nil {
		return nil, fmt.Errorf("failed to set prefetch: %w", err)
	}
	return channel, nil
}

// subscribe merges every queue into one job stream, which closes once all
// queues stop delivering (unsubscribed or connection lost).
func subscribe(channel *amqp.Channel, handlers map[string]deliveryHandler) (<-chan job, error) {
	jobs := make(chan job)
	var forwarders sync.WaitGroup

	for queue, handle := range handlers {
		deliveries, err := consume(channel, queue)
		if err != nil {
			return nil, err
		}
		forwarders.Add(1)
		go func() {
			defer forwarders.Done()
			for delivery := range deliveries {
				jobs <- job{handle: handle, delivery: delivery}
			}
		}()
	}

	go func() {
		forwarders.Wait()
		close(jobs)
	}()
	return jobs, nil
}

func (c *Consumer) handlersByQueue() map[string]deliveryHandler {
	handlers := map[string]deliveryHandler{rabbitmq.DeadLetterQueue: logDeadLetter}
	for _, notificationType := range notification.Types() {
		handlers[rabbitmq.QueueForType(notificationType)] = c.processor.Process
	}
	return handlers
}

// Each queue is consumed under its own name as the consumer tag, which is what
// unsubscribe cancels.
func consume(channel *amqp.Channel, queue string) (<-chan amqp.Delivery, error) {
	const autoAck, exclusive, noLocal, noWait = false, false, false, false
	deliveries, err := channel.Consume(queue, queue, autoAck, exclusive, noLocal, noWait, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to consume from %s: %w", queue, err)
	}
	return deliveries, nil
}

// unsubscribe stops new deliveries while keeping the channel open, so
// in-flight notifications can still be acknowledged.
func unsubscribe(channel *amqp.Channel, handlers map[string]deliveryHandler) {
	const noWait = false
	for queue := range handlers {
		if err := channel.Cancel(queue, noWait); err != nil {
			log.Printf("error cancelling consumer for %s: %v", queue, err)
		}
	}
}

func (c *Consumer) startWorkers(jobs <-chan job) <-chan struct{} {
	var workers sync.WaitGroup
	for range c.workers {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for queued := range jobs {
				runJob(queued)
			}
		}()
	}

	done := make(chan struct{})
	go func() {
		workers.Wait()
		close(done)
	}()
	return done
}

// A job outlives shutdown on purpose: an in-flight notification is finished
// and settled rather than abandoned half-sent.
func runJob(queued job) {
	ctx, cancel := context.WithTimeout(context.Background(), processingTimeout)
	defer cancel()
	queued.handle(ctx, queued.delivery)
}

func logDeadLetter(_ context.Context, delivery amqp.Delivery) {
	log.Printf("Received DLQ message: %s", delivery.Body)
	acknowledge(delivery)
}

func closeQuietly(conn *amqp.Connection) {
	if err := conn.Close(); err != nil && !errors.Is(err, amqp.ErrClosed) {
		log.Printf("error closing consumer connection: %v", err)
	}
}
