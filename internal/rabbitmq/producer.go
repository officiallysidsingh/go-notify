package rabbitmq

import (
	"fmt"

	"github.com/streadway/amqp"
)

type RabbitMQProducer struct {
	conn    *amqp.Connection
	channel *amqp.Channel
	queue   amqp.Queue
}

// Init RabbitMQ Producer
func NewProducer(url, queueName string) (*RabbitMQProducer, error) {
	conn, err := amqp.Dial(url)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to RabbitMQ: %w", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("failed to open a channel: %w", err)
	}

	q, err := ch.QueueDeclare(
		queueName,
		true,
		false,
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
