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
	return p.channel.Publish(
		"",
		p.queue.Name,
		false,
		false,
		amqp.Publishing{
			ContentType:  "text/plain",
			Body:         []byte(message),
			DeliveryMode: amqp.Persistent, // Ensure messages survive RabbitMQ restarts
		},
	)
}

// To shut down connection
func (p *RabbitMQProducer) Close() {
	p.channel.Close()
	p.conn.Close()
}
