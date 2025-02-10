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

	// Declare/Ensure queue exists
	_, err = ch.QueueDeclare(
		config.AppConfig.RabbitMQ.Queue,
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
	dbConn := repository.NewDB(config.AppConfig.Postgres.DSN)

	// Consume messages.
	msgs, err := ch.Consume(
		config.AppConfig.RabbitMQ.Queue,
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
			err = service.SendPushNotification(
				config.AppConfig.Ntfy.Topic,
				notifMsg.Title,
				notifMsg.Priority,
				notifMsg.Message,
			)
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
