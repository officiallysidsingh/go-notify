package grpc

import (
	"context"
	"fmt"
	"log"

	pb "github.com/officiallysidsingh/go-notify/api/generated"
	"github.com/officiallysidsingh/go-notify/internal/db"
	"github.com/officiallysidsingh/go-notify/internal/rabbitmq"
)

type NotificationServer struct {
	pb.UnimplementedNotificationServiceServer
	producer *rabbitmq.RabbitMQProducer
	db       *db.DB
}

// Init gRPC server
func NewNotificationServer(producer *rabbitmq.RabbitMQProducer, db *db.DB) *NotificationServer {
	return &NotificationServer{producer: producer, db: db}
}

// To handle notification requests
func (s *NotificationServer) SendNotification(
	ctx context.Context,
	req *pb.NotificationRequest,
) (
	*pb.NotificationResponse,
	error,
) {
	log.Printf("Received notification request for user: %s", req.UserId)

	err := s.db.InsertNotification(req.UserId, req.Message, "pending")
	if err != nil {
		return &pb.NotificationResponse{Success: false, Error: err.Error()}, err
	}

	err = s.producer.Publish(req.Message)
	if err != nil {
		return &pb.NotificationResponse{Success: false, Error: err.Error()}, err
	}

	return &pb.NotificationResponse{Success: true}, nil
}

func (s *NotificationServer) GetNotificationStatus(
	ctx context.Context,
	req *pb.StatusRequest,
) (
	*pb.StatusResponse,
	error,
) {
	var status string
	query := "SELECT status FROM notifications WHERE id=$1"

	err := s.db.Conn.Get(&status, query, req.NotificationId)
	if err != nil {
		return &pb.StatusResponse{
			Status: "",
			Error:  fmt.Sprintf("Notification not found: %v", err.Error()),
		}, err
	}

	return &pb.StatusResponse{
		Status: status,
	}, nil
}
