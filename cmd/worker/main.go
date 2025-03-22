package main

import (
	"encoding/json"
	"log"
	"time"

	"github.com/officiallysidsingh/go-notify/config"
	"github.com/officiallysidsingh/go-notify/internal/repository"
	"github.com/officiallysidsingh/go-notify/internal/service"
	"github.com/streadway/amqp"
)

// struct of payload from RabbitMQ
type NotificationMessage struct {
	NotificationID int64  `json:"notification_id"`
	UserID         string `json:"user_id"`
	Title          string `json:"title"`
	Priority       string `json:"priority"`
	Message        string `json:"message"`
	Type           string `json:"type"`
}

func main() {
	// Load configuration from the config folder
	config.LoadConfig("./config")

	// Connect to RabbitMQ
	conn, err := amqp.Dial(config.AppConfig.RabbitMQ.URL)
	if err != nil {
		log.Fatalf("Failed to connect to RabbitMQ: %v", err)
	}
	defer conn.Close()

	// Open RabbitMQ Channel
	ch, err := conn.Channel()
	if err != nil {
		log.Fatalf("Failed to open a RabbitMQ channel: %v", err)
	}
	defer ch.Close()

	// Enable Dead Lettering
	queueArgs := amqp.Table{
		"x-dead-letter-exchange":    "dead_letter_exchange",
		"x-dead-letter-routing-key": "dead_letter",
	}

	// Define queues
	queues := []string{
		"dead_letter_queue",
		"queue_email",
		"queue_sms",
		"queue_push",
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
		_, err := ch.QueueDeclare(
			queueName,
			true,
			false,
			false,
			false,
			args,
		)
		if err != nil {
			log.Fatalf("Failed to declare queue %s: %v", queueName, err)
		}

		// Consume messages
		msgs, err := ch.Consume(
			queueName,
			"",
			false,
			false,
			false,
			false,
			nil,
		)
		if err != nil {
			log.Fatalf("Failed to register consumer for queue %s: %v", queueName, err)
		}

		// Launch a goroutine to process messages from queue
		go consumeMessages(queueName, msgs)
	}

	log.Println("Worker is up and running, waiting for messages...")
	forever := make(chan bool)
	<-forever
}

// Processe messages from a given queue
func consumeMessages(queueName string, msgs <-chan amqp.Delivery) {
	// Connect to PostgreSQL
	dbConn := repository.NewDB(config.AppConfig.Postgres.DSN)

	for d := range msgs {
		// In DLQ, simply log the message for manual intervention
		if queueName == "dead_letter_queue" {
			log.Printf("Received DLQ message: %s", string(d.Body))
			// Acknowledge the message to remove it from the DLQ
			if err := d.Ack(false); err != nil {
				log.Printf("Error acknowledging DLQ message: %v", err)
			}
			continue
		}

		// For main queues, unmarshal the message
		var notifMsg NotificationMessage
		err := json.Unmarshal(d.Body, &notifMsg)
		if err != nil {
			log.Printf("Error unmarshaling message from queue %s: %v", queueName, err)
			// Reject the message without requeueing
			if nackErr := d.Nack(false, false); nackErr != nil {
				log.Printf("Error sending Nack for queue %s: %v", queueName, nackErr)
			}
			continue
		}

		log.Printf(
			"Processing notification %d for user %s from queue %s",
			notifMsg.NotificationID,
			notifMsg.UserID,
			queueName,
		)

		// Process the message based on which queue it came from
		switch queueName {
		case "queue_email":
			// TODO
			// err = service.SendEmailNotification(notifMsg)
			println("Make SendEmailNotification Service")
		case "queue_sms":
			// TODO
			// err = service.SendSMSNotification(notifMsg)
			println("Make SendSMSNotification Service")
		case "queue_push":
			err = service.SendPushNotification(
				config.AppConfig.Ntfy.Topic,
				notifMsg.Title,
				notifMsg.Priority,
				notifMsg.Message,
			)
		default:
			log.Printf("Unknown queue: %s", queueName)
		}
		if err != nil {
			log.Printf(
				"Failed to process notification %d from queue %s: %v",
				notifMsg.NotificationID,
				queueName,
				err,
			)

			// Update DB status to "failed" and Nack the message
			if errUpdate := dbConn.UpdateNotificationStatus(notifMsg.NotificationID, "failed"); errUpdate != nil {
				log.Printf("Failed to update notification status: %v", errUpdate)
			}

			// Requeue the message after a short delay
			time.Sleep(2 * time.Second)
			if nackErr := d.Nack(false, true); nackErr != nil {
				log.Printf("Error sending Nack for queue %s: %v", queueName, nackErr)
			}
			continue
		}

		// Update DB status to "sent" on successful processing
		if err := dbConn.UpdateNotificationStatus(notifMsg.NotificationID, "sent"); err != nil {
			log.Printf(
				"Failed to update notification status for notification %d: %v",
				notifMsg.NotificationID,
				err,
			)
			if nackErr := d.Nack(false, true); nackErr != nil {
				log.Printf("Error sending Nack for queue %s: %v", queueName, nackErr)
			}
			continue
		}

		// Acknowledge successful processing.
		if ackErr := d.Ack(false); ackErr != nil {
			log.Printf("Error sending Ack for queue %s: %v", queueName, ackErr)
		} else {
			log.Printf("Notification %d processed and sent from queue %s.", notifMsg.NotificationID, queueName)
		}
	}
}
