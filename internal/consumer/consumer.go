package consumer

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/officiallysidsingh/go-notify/config"
	"github.com/officiallysidsingh/go-notify/internal/repository"
	"github.com/officiallysidsingh/go-notify/internal/service"
	"github.com/streadway/amqp"
)

// Payload from RabbitMQ
type NotificationMessage struct {
	NotificationID int64  `json:"notification_id"`
	UserID         string `json:"user_id"`
	Title          string `json:"title"`
	Priority       string `json:"priority"`
	Message        string `json:"message"`
	Type           string `json:"type"`
}

// Message wraps a RabbitMQ delivery with its queue name
type Message struct {
	QueueName string
	Delivery  amqp.Delivery
}

// Consumer encapsulates the logic for consuming messages
type Consumer struct {
	conn       *amqp.Connection
	ch         *amqp.Channel
	dbConn     *repository.DB
	msgChannel chan Message
	workers    int
	wg         sync.WaitGroup
}

// Create a new Consumer instance
func NewConsumer(amqpURL string, workers int, db *repository.DB) (*Consumer, error) {
	// Connect to RabbitMQ
	conn, err := amqp.Dial(amqpURL)
	if err != nil {
		return nil, err
	}

	ch, err := conn.Channel()
	if err != nil {
		if err := conn.Close(); err != nil {
			log.Printf("error closing connection: %v", err)
		}
		return nil, err
	}

	return &Consumer{
		conn:       conn,
		ch:         ch,
		dbConn:     db,
		workers:    workers,
		msgChannel: make(chan Message, 100),
	}, nil
}

// Consume messages from multiple queues
func (c *Consumer) Start(queues []string) error {
	// Enable Dead Lettering
	queueArgs := amqp.Table{
		"x-dead-letter-exchange":    "dead_letter_exchange",
		"x-dead-letter-routing-key": "dead_letter",
	}

	// Declare and start a consumer for each queue
	for _, queueName := range queues {
		var args amqp.Table

		// No args in dead_letter_queue
		if queueName == "dead_letter_queue" {
			args = nil
		} else {
			args = queueArgs
		}

		// Declare the queue to ensure it exists
		_, err := c.ch.QueueDeclare(
			queueName,
			true,
			false,
			false,
			false,
			args,
		)
		if err != nil {
			return err
		}

		// Consume messages
		msgs, err := c.ch.Consume(
			queueName,
			"",
			false,
			false,
			false,
			false,
			nil,
		)
		if err != nil {
			return err
		}

		// Push messages from each queue into the global msgChannel
		go func(q string, deliveries <-chan amqp.Delivery) {
			for d := range deliveries {
				c.msgChannel <- Message{QueueName: q, Delivery: d}
			}
		}(queueName, msgs)
	}

	// Start worker goroutines.
	for i := 0; i < c.workers; i++ {
		c.wg.Add(1)
		go c.worker()
	}

	return nil
}

// Process messages from the global msgChannel
func (c *Consumer) worker() {
	defer c.wg.Done()
	for msg := range c.msgChannel {
		c.processMessage(msg)
	}
}

// Handle single message with its own context
func (c *Consumer) processMessage(msg Message) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// In DLQ, simply log the message for manual intervention
	if msg.QueueName == "dead_letter_queue" {
		log.Printf("Received DLQ message: %s", string(msg.Delivery.Body))
		// Acknowledge the message to remove it from the DLQ
		if err := msg.Delivery.Ack(false); err != nil {
			log.Printf("Error acknowledging DLQ message: %v", err)
		}
		return
	}

	// For main queues, unmarshal the message
	var notifMsg NotificationMessage
	err := json.Unmarshal(msg.Delivery.Body, &notifMsg)
	if err != nil {
		log.Printf("Error unmarshaling message from queue %s: %v", msg.QueueName, err)
		// Reject the message without requeueing
		if nackErr := msg.Delivery.Nack(false, false); nackErr != nil {
			log.Printf("Error sending Nack for queue %s: %v", msg.QueueName, nackErr)
		}
		return
	}

	log.Printf("Processing notification %d from %s", notifMsg.NotificationID, msg.QueueName)

	// Process the message based on which queue it came from
	switch msg.QueueName {
	case "queue_email":
		// TODO: Implement email notification
		println("Make SendEmailNotification Service")
	case "queue_sms":
		// TODO: Implement SMS notification
		println("Make SendSMSNotification Service")
	case "queue_push":
		err = service.SendPushNotification(
			config.AppConfig.Ntfy.Topic,
			notifMsg.Title,
			notifMsg.Priority,
			notifMsg.Message,
		)
	default:
		log.Printf("Unknown queue: %s", msg.QueueName)
	}

	if err != nil {
		log.Printf(
			"Failed to process notification %d from queue %s: %v",
			notifMsg.NotificationID,
			msg.QueueName,
			err,
		)

		// For updating the status to "failed"
		err = c.dbConn.UpdateNotificationStatus(ctx, notifMsg.NotificationID, "failed")
		if err != nil {
			log.Printf("Failed updating status: %v", err)
		}

		// Requeue the message after a short delay
		time.Sleep(2 * time.Second)
		err = msg.Delivery.Nack(false, true)
		if err != nil {
			log.Printf("Error sending Nack for queue %s: %v", msg.QueueName, err)
		}
		return
	}

	// Update DB status to "sent" on successful processing
	if err := c.dbConn.UpdateNotificationStatus(ctx, notifMsg.NotificationID, "sent"); err != nil {
		log.Printf(
			"Failed to update notification status for notification %d: %v",
			notifMsg.NotificationID,
			err,
		)
		if err := msg.Delivery.Nack(false, true); err != nil {
			log.Printf("Error sending Nack for queue %s: %v", msg.QueueName, err)
		}
		return
	}

	// Acknowledge successful processing
	if err := msg.Delivery.Ack(false); err != nil {
		log.Printf("Error sending Ack for queue %s: %v", msg.QueueName, err)
	} else {
		log.Printf("Notification %d processed and sent from queue %s.", notifMsg.NotificationID, msg.QueueName)
	}
}

// Stop gracefully shuts down the consumer.
func (c *Consumer) Stop() {
	close(c.msgChannel)
	c.wg.Wait()

	if err := c.ch.Close(); err != nil {
		log.Printf("error closing channel: %v", err)
	}

	if err := c.conn.Close(); err != nil {
		log.Printf("error closing consumer connection: %v", err)
	}
}
