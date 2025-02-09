package grpc

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	pb "github.com/officiallysidsingh/go-notify/api/generated"
	"github.com/officiallysidsingh/go-notify/internal/rabbitmq"
	"github.com/officiallysidsingh/go-notify/internal/repository"
)

// NotificationMessage defines the payload published to RabbitMQ.
type NotificationMessage struct {
	NotificationID int64  `json:"notification_id"`
	UserID         string `json:"user_id"`
	Title          string `json:"title"`
	Priority       string `json:"priority"`
	Message        string `json:"message"`
}

type NotificationServer struct {
	pb.UnimplementedNotificationServiceServer
	producer *rabbitmq.RabbitMQProducer
	db       *repository.DB
}

// Init gRPC server
func NewNotificationServer(
	producer *rabbitmq.RabbitMQProducer,
	db *repository.DB,
) *NotificationServer {
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

	// Insert notification into db
	notificationID, err := s.db.InsertNotification(req.UserId, req.Message, "pending")
	if err != nil {
		return &pb.NotificationResponse{Success: false, Error: err.Error()}, err
	}

	// Prepare payload
	payload := NotificationMessage{
		NotificationID: notificationID,
		UserID:         req.UserId,
		Title:          req.Title,
		Priority:       req.Priority,
		Message:        req.Message,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return &pb.NotificationResponse{Success: false, Error: err.Error()}, err
	}

	// Publish the payload to RabbitMQ
	err = s.producer.Publish(string(data))
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
