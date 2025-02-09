package main

import (
	"encoding/json"
	"log"
	"time"

	"github.com/officiallysidsingh/go-notify/internal/repository"
	"github.com/officiallysidsingh/go-notify/internal/service"
	"github.com/streadway/amqp"
)

// struct of payload from RabbitMQ
type NotificationMessage struct {
	NotificationID int64  `json:"notification_id"`
	UserID         string `json:"user_id"`
	Message        string `json:"message"`
}

const (
	rabbitMQURL = "amqp://guest:guest@localhost:5672/"
	queueName   = "notifications"
	postgresDSN = "postgres://notify:notify_pass@localhost:5432/go_notify?sslmode=disable"
	ntfyTopic   = "go-notify-sid"
)

func main() {
	// Connect to RabbitMQ
	conn, err := amqp.Dial(rabbitMQURL)
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

	// Declare/Ensure queue exists
	_, err = ch.QueueDeclare(
		queueName,
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		log.Fatalf("Failed to declare RabbitMQ queue: %v", err)
	}

	// Connect to PostgreSQL.
	dbConn := repository.NewDB(postgresDSN)

	// Consume messages.
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
		log.Fatalf("Failed to register RabbitMQ consumer: %v", err)
	}

	forever := make(chan bool)

	// Process messages concurrently.
	go func() {
		for d := range msgs {
			var notifMsg NotificationMessage
			err := json.Unmarshal(d.Body, &notifMsg)
			if err != nil {
				log.Printf("Error unmarshaling message: %v", err)
				d.Nack(false, false)
				continue
			}

			log.Printf(
				"Processing notification %d for user %s",
				notifMsg.NotificationID,
				notifMsg.UserID,
			)

			// Send push notification using ntfy.
			err = service.SendPushNotification(ntfyTopic, "New Notification", notifMsg.Message)
			if err != nil {
				log.Printf("Failed to send push notification: %v", err)
				// Update DB status to "failed" and Nack the message.
				if errUpdate := dbConn.UpdateNotificationStatus(notifMsg.NotificationID, "failed"); errUpdate != nil {
					log.Printf("Failed to update notification status: %v", errUpdate)
				}
				// Requeue the message after a short delay.
				time.Sleep(2 * time.Second)
				d.Nack(false, true)
				continue
			}

			// Update DB status to "sent".
			if err := dbConn.UpdateNotificationStatus(notifMsg.NotificationID, "sent"); err != nil {
				log.Printf("Failed to update notification status: %v", err)
				d.Nack(false, true)
				continue
			}

			// Acknowledge successful processing.
			d.Ack(false)
			log.Printf("Notification %d processed and sent.", notifMsg.NotificationID)
		}
	}()

	log.Println("Worker is up and running, waiting for messages...")
	<-forever
}
