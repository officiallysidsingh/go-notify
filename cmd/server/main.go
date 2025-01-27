package main

import (
	"fmt"
	"log"
	"net"

	pb "github.com/officiallysidsingh/go-notify/api/generated"
	grpcserver "github.com/officiallysidsingh/go-notify/internal/grpc"
	"github.com/officiallysidsingh/go-notify/internal/rabbitmq"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

const (
	grpcPort    = ":50051"
	rabbitMQURL = "amqp://guest:guest@localhost:5672/"
	queueName   = "notifications"
)

func main() {
	// Init RabbitMQ Producer
	producer, err := rabbitmq.NewProducer(rabbitMQURL, queueName)
	if err != nil {
		log.Fatalf("Failed to initialize RabbitMQ: %v", err)
	}
	defer producer.Close()

	// Start gRPC Server
	listener, err := net.Listen("tcp", grpcPort)
	if err != nil {
		log.Fatalf("Failed to listen on port %s: %v", grpcPort, err)
	}

	server := grpcserver.NewNotificationServer(producer)

	grpcServer := grpc.NewServer()
	pb.RegisterNotificationServiceServer(grpcServer, server)

	reflection.Register(grpcServer)

	fmt.Printf("gRPC server is running on port %s\n", grpcPort)
	if err := grpcServer.Serve(listener); err != nil {
		log.Fatalf("Failed to serve gRPC server: %v", err)
	}
}
