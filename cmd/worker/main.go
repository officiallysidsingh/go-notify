package main

import (
	"log"

	"github.com/officiallysidsingh/go-notify/config"
	"github.com/officiallysidsingh/go-notify/internal/consumer"
	"github.com/officiallysidsingh/go-notify/internal/repository"
)

func main() {
	// Load configuration from the config folder
	config.LoadConfig("./config")

	// Connect to PostgreSQL
	dbConn, err := repository.NewDB(config.AppConfig.Postgres)
	if err != nil {
		log.Fatalf("Failed to initialize PostgresDB: %v", err)
	}
	defer dbConn.Close()

	// Create a new consumer with a global worker pool
	consumer, err := consumer.NewConsumer(config.AppConfig.RabbitMQ.URL, 10, dbConn)
	if err != nil {
		log.Fatalf("Failed to initialize consumer: %v", err)
	}

	// Define queues
	queues := []string{
		"dead_letter_queue",
		"queue_email",
		"queue_sms",
		"queue_push",
	}

	// Start the consumer
	if err := consumer.Start(queues); err != nil {
		log.Fatalf("Failed to start consumer: %v", err)
	}

	log.Println("Consumer is up and running, waiting for messages...")

	// Block forever
	select {}
}
