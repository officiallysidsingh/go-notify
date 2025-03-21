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
}

// Init RabbitMQ Producer
func NewProducer(url string) (*RabbitMQProducer, error) {
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

	return &RabbitMQProducer{
		conn:    conn,
		channel: ch,
	}, nil
}

// Declare necessary exchanges and queues
func (p *RabbitMQProducer) SetupExchangesAndQueues() error {
	// Declaring Dead Letter Exchange (DLX)
	if err := p.channel.ExchangeDeclare(
		"dead_letter_exchange",
		"direct",
		true,
		false,
		false,
		false,
		nil,
	); err != nil {
		return err
	}

	// Declaring Dead Letter Queue (DLQ)
	_, err := p.channel.QueueDeclare(
		"dead_letter_queue",
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		return err
	}

	// Bind DLQ to DLX with fixed routing key
	if err := p.channel.QueueBind(
		"dead_letter_queue",
		"dead_letter",
		"dead_letter_exchange",
		false,
		nil,
	); err != nil {
		return err
	}

	// Declaring Topic Exchange for Notification Type based routing
	if err := p.channel.ExchangeDeclare(
		"notification_exchange_topic",
		"topic",
		true,
		false,
		false,
		false,
		nil,
	); err != nil {
		return err
	}

	// Declaring Fanout Exchange for broadcasting to all channels
	if err := p.channel.ExchangeDeclare(
		"notification_exchange_fanout",
		"fanout",
		true,
		false,
		false,
		false,
		nil,
	); err != nil {
		return err
	}

	// Enable Dead Lettering
	queueArgs := amqp.Table{
		"x-dead-letter-exchange":    "dead_letter_exchange",
		"x-dead-letter-routing-key": "dead_letter",
	}

	// Queues for each notification type
	queues := []struct {
		Name       string
		BindingKey string
	}{
		{"queue_email", "email"},
		{"queue_sms", "sms"},
		{"queue_push", "push"},
	}

	for _, q := range queues {
		// Declare each notification type queue
		_, err := p.channel.QueueDeclare(
			q.Name,
			true,
			false,
			false,
			false,
			queueArgs,
		)
		if err != nil {
			return err
		}

		// Bind queue to topic exchange
		if err := p.channel.QueueBind(
			q.Name,
			q.BindingKey,
			"notification_exchange_topic",
			false,
			nil,
		); err != nil {
			return err
		}

		// Bind queue to fanout exchange
		if err := p.channel.QueueBind(
			q.Name,
			"",
			"notification_exchange_fanout",
			false,
			nil,
		); err != nil {
			return err
		}
	}

	return nil
}

// To send a message to the queue
func (p *RabbitMQProducer) Publish(exchange, routingKey, message string) error {
	var err error

	// Retry upto 3 times
	for i := 0; i < 3; i++ {
		err = p.channel.Publish(
			exchange,
			routingKey,
			false,
			false,
			amqp.Publishing{
				ContentType:  "application/json",
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
	if p.channel != nil {
		p.channel.Close()
	}
	if p.conn != nil {
		p.conn.Close()
	}
}
