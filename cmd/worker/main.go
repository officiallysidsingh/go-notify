package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"
	"time"

	"github.com/officiallysidsingh/go-notify/config"
	"github.com/officiallysidsingh/go-notify/internal/consumer"
	"github.com/officiallysidsingh/go-notify/internal/notification"
	"github.com/officiallysidsingh/go-notify/internal/repository"
	"github.com/officiallysidsingh/go-notify/internal/service"
)

const (
	workerCount    = 10
	sendAttempts   = 3
	sendRetryDelay = 2 * time.Second
)

func main() {
	cfg, err := config.Load("./config")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	database, err := repository.NewDB(cfg.Postgres)
	if err != nil {
		log.Fatalf("Failed to initialize PostgresDB: %v", err)
	}
	defer func() {
		if err := database.Close(); err != nil {
			log.Printf("error closing database: %v", err)
		}
	}()

	// Email and SMS have no sender yet; the processor fails and dead-letters
	// those notifications instead of reporting them as sent.
	senders := map[string]consumer.Sender{
		notification.TypePush: service.NewNtfySender(cfg.Ntfy),
	}
	retry := consumer.RetryPolicy{MaxAttempts: sendAttempts, Delay: sendRetryDelay}
	processor := consumer.NewProcessor(database, senders, retry)

	consumer.NewConsumer(cfg.RabbitMQ.URL, workerCount, processor).Run(ctx)
	log.Println("Worker stopped")
}
