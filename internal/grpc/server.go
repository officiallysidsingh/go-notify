package grpc

import (
	"context"
	"log"

	pb "github.com/officiallysidsingh/go-notify/api/generated"
	"github.com/officiallysidsingh/go-notify/internal/rabbitmq"
)

type NotificationServer struct {
	pb.UnimplementedNotificationServiceServer
	producer *rabbitmq.RabbitMQProducer
}

// Init gRPC server
func NewNotificationServer(producer *rabbitmq.RabbitMQProducer) *NotificationServer {
	return &NotificationServer{producer: producer}
}

// To handle notification requests
func (s *NotificationServer) SendNotification(
	ctx context.Context,
	req *pb.NotificationRequest,
) (*pb.NotificationResponse, error) {
	log.Printf("Received notification request for user: %s", req.UserId)

	err := s.producer.Publish(req.Message)
	if err != nil {
		return &pb.NotificationResponse{Success: false, Error: err.Error()}, err
	}

	return &pb.NotificationResponse{Success: true}, nil
}
