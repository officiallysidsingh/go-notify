package rabbitmq

import (
	"fmt"
	"log"
	"time"

	"github.com/streadway/amqp"
)

type RabbitMQProducer struct {
	conn    *amqp.Connection
	channel *amqp.Channel
	queue   amqp.Queue
}

// Init RabbitMQ Producer
func NewProducer(url, queueName string) (*RabbitMQProducer, error) {
	var conn *amqp.Connection
	var err error

	// Retry connection up to 5 times
	for i := 0; i < 5; i++ {
		conn, err = amqp.Dial(url)
		if err == nil {
			break
		}
		log.Printf("RabbitMQ connection failed. Retrying... (%d/5)", i+1)
		time.Sleep(2 * time.Second)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to connect to RabbitMQ: %w", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("failed to open a channel: %w", err)
	}

	q, err := ch.QueueDeclare(
		queueName,
		true,  // Durable queue
		false, // Do not auto-delete
		false,
		false,
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to declare a queue: %w", err)
	}

	return &RabbitMQProducer{conn, ch, q}, nil
}

// To send a message to the queue
func (p *RabbitMQProducer) Publish(message string) error {
	var err error

	// Retry upto 3 times
	for i := 0; i < 3; i++ {
		err = p.channel.Publish(
			"",
			p.queue.Name,
			false,
			false,
			amqp.Publishing{
				ContentType:  "text/plain",
				Body:         []byte(message),
				DeliveryMode: amqp.Persistent,
			},
		)
		if err == nil {
			return nil
		}

		log.Printf("Failed to publish message. Retrying... (%d/3)", i+1)

		// Wait before retrying
		time.Sleep(1 * time.Second)
	}

	return fmt.Errorf("failed to publish message after retries: %w", err)
}

// To shut down connection
func (p *RabbitMQProducer) Close() {
	p.channel.Close()
	p.conn.Close()
}
