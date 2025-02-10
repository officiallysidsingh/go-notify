package main

import (
	"fmt"
	"log"
	"net"

	"github.com/officiallysidsingh/go-notify/config"

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

	// Init RabbitMQ Producer
	producer, err := rabbitmq.NewProducer(
		config.AppConfig.RabbitMQ.URL,
		config.AppConfig.RabbitMQ.Queue,
	)
	if err != nil {
		log.Fatalf("Failed to initialize RabbitMQ: %v", err)
	}
	defer producer.Close()

	// Init DB Connection
	database := repository.NewDB(config.AppConfig.Postgres.DSN)

	// Start gRPC Server
	listener, err := net.Listen("tcp", config.AppConfig.GRPC.Port)
	if err != nil {
		log.Fatalf("Failed to listen on port %s: %v", config.AppConfig.GRPC.Port, err)
	}

	server := grpcserver.NewNotificationServer(producer, database)

	grpcServer := grpc.NewServer()
	pb.RegisterNotificationServiceServer(grpcServer, server)

	reflection.Register(grpcServer)

	fmt.Printf("gRPC server is running on port %s\n", config.AppConfig.GRPC.Port)
	if err := grpcServer.Serve(listener); err != nil {
		log.Fatalf("Failed to serve gRPC server: %v", err)
	}
}
