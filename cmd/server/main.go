package main

import (
	"log"
	"net"
	"net/http"
	"time"

	"github.com/officiallysidsingh/go-notify/config"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"

	pb "github.com/officiallysidsingh/go-notify/api/generated"
	grpcserver "github.com/officiallysidsingh/go-notify/internal/grpc"
	"github.com/officiallysidsingh/go-notify/internal/producer"
	"github.com/officiallysidsingh/go-notify/internal/ratelimiter"
	"github.com/officiallysidsingh/go-notify/internal/repository"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
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
	sugar := logger.Sugar()
	defer func() {
		if err := logger.Sync(); err != nil {
			sugar.Errorw("failed to sync logger", "error", err)
		}
	}()

	// Start Prometheus metrics HTTP server in separate goroutine
	go func() {
		sugar.Infof("Starting metrics server on %s", config.AppConfig.Metrics.Port)
		http.Handle("/metrics", promhttp.Handler())
		if err := http.ListenAndServe(config.AppConfig.Metrics.Port, nil); err != nil {
			sugar.Fatalf("Metrics HTTP server failed: %v", err)
		}
	}()

	// Init RabbitMQ Producer
	producer, err := producer.NewProducer(
		config.AppConfig.RabbitMQ.URL,
	)
	if err != nil {
		sugar.Fatalf("Failed to initialize RabbitMQ: %v", err)
	}
	defer producer.Close()

	// Connect to Postgres DB
	database, err := repository.NewDB(config.AppConfig.Postgres)
	if err != nil {
		sugar.Fatalf("Failed to initialize PostgresDB: %v", err)
	}

	defer func() {
		if err := database.Close(); err != nil {
			log.Printf("error closing database: %v", err)
		}
	}()

	// Convert the Redis window from string to time.Duration
	redisWindowDuration, err := time.ParseDuration(config.AppConfig.Redis.Window)
	if err != nil {
		log.Fatalf("Invalid Redis window duration: %v", err)
	}

	// Connect to Rate Limiter
	limiter := ratelimiter.NewRateLimiter(
		config.AppConfig.Redis.Addr,
		config.AppConfig.Redis.Limit,
		redisWindowDuration,
	)

	// Start gRPC Server
	listener, err := net.Listen("tcp", config.AppConfig.GRPC.Port)
	if err != nil {
		sugar.Fatalf("Failed to listen on port %s: %v", config.AppConfig.GRPC.Port, err)
	}

	// Create gRPC server with integrated notification service
	server := grpcserver.NewNotificationServer(producer, database, limiter)
	grpcServer := grpc.NewServer()
	pb.RegisterNotificationServiceServer(grpcServer, server)

	// Register gRPC health service
	healthServer := health.NewServer()
	grpc_health_v1.RegisterHealthServer(grpcServer, healthServer)

	// Mark the server as serving
	healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)

	// Register reflection service for debugging
	reflection.Register(grpcServer)

	sugar.Infof("gRPC server running on %s", config.AppConfig.GRPC.Port)
	if err := grpcServer.Serve(listener); err != nil {
		sugar.Fatalf("Failed to serve gRPC server: %v", err)
	}
}
