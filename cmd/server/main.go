package main

import (
	"net"
	"net/http"

	"github.com/officiallysidsingh/go-notify/config"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"

	pb "github.com/officiallysidsingh/go-notify/api/generated"
	grpcserver "github.com/officiallysidsingh/go-notify/internal/grpc"
	"github.com/officiallysidsingh/go-notify/internal/rabbitmq"
	"github.com/officiallysidsingh/go-notify/internal/repository"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

func main() {
	// Load configuration from the config folder.
	config.LoadConfig("./config")

	// Structured logging
	logger, err := zap.NewProduction()
	if err != nil {
		panic(err)
	}
	defer logger.Sync()
	sugar := logger.Sugar()

	// Start Prometheus metrics HTTP server in separate goroutine
	go func() {
		sugar.Infof("Starting metrics server on %s", config.AppConfig.Metrics.Port)
		http.Handle("/metrics", promhttp.Handler())
		if err := http.ListenAndServe(config.AppConfig.Metrics.Port, nil); err != nil {
			sugar.Fatalf("Metrics HTTP server failed: %v", err)
		}
	}()

	// Init RabbitMQ Producer
	producer, err := rabbitmq.NewProducer(
		config.AppConfig.RabbitMQ.URL,
		config.AppConfig.RabbitMQ.Queue,
	)
	if err != nil {
		sugar.Fatalf("Failed to initialize RabbitMQ: %v", err)
	}
	defer producer.Close()

	// Connect to Postgres DB
	database := repository.NewDB(config.AppConfig.Postgres.DSN)

	// Start gRPC Server
	listener, err := net.Listen("tcp", config.AppConfig.GRPC.Port)
	if err != nil {
		sugar.Fatalf("Failed to listen on port %s: %v", config.AppConfig.GRPC.Port, err)
	}

	// Create gRPC server with integrated notification service
	server := grpcserver.NewNotificationServer(producer, database)
	grpcServer := grpc.NewServer()
	pb.RegisterNotificationServiceServer(grpcServer, server)

	reflection.Register(grpcServer)

	sugar.Infof("gRPC server running on %s", config.AppConfig.GRPC.Port)
	if err := grpcServer.Serve(listener); err != nil {
		sugar.Fatalf("Failed to serve gRPC server: %v", err)
	}
}
