package grpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"

	"github.com/prometheus/client_golang/prometheus"

	pb "github.com/officiallysidsingh/go-notify/api/generated"
	"github.com/officiallysidsingh/go-notify/internal/producer"
	"github.com/officiallysidsingh/go-notify/internal/ratelimiter"
	"github.com/officiallysidsingh/go-notify/internal/repository"
)

// NotificationMessage defines the payload published to RabbitMQ.
type NotificationMessage struct {
	NotificationID int64  `json:"notification_id"`
	UserID         string `json:"user_id"`
	Title          string `json:"title"`
	Priority       string `json:"priority"`
	Message        string `json:"message"`
	Type           string `json:"type"`
}

// Prometheus total notification counter
var notificationsReceived = prometheus.NewCounter(
	prometheus.CounterOpts{
		Name: "notifications_received_total",
		Help: "Total number of notifications received via gRPC",
	},
)

type NotificationServer struct {
	pb.UnimplementedNotificationServiceServer
	producer    *producer.RabbitMQProducer
	db          *repository.DB
	rateLimiter *ratelimiter.RateLimiter
}

// Init prometheus counter
func init() {
	prometheus.MustRegister(notificationsReceived)
}

// Init gRPC server
func NewNotificationServer(
	producer *producer.RabbitMQProducer,
	db *repository.DB,
	limiter *ratelimiter.RateLimiter,
) *NotificationServer {
	return &NotificationServer{
		producer:    producer,
		db:          db,
		rateLimiter: limiter,
	}
}

// To handle notification requests
func (s *NotificationServer) SendNotification(
	ctx context.Context,
	req *pb.NotificationRequest,
) (
	*pb.NotificationResponse,
	error,
) {
	// Rate limiting
	allowed, err := s.rateLimiter.Allow(ctx, req.UserId)
	if err != nil {
		log.Printf("Rate limiter error: %v", err)
		return &pb.NotificationResponse{
				Success: false,
				Error:   "Rate limiter error",
			}, errors.New(
				"Rate limiter error",
			)
	}
	if !allowed {
		return &pb.NotificationResponse{
				Success: false,
				Error:   "Rate limit exceeded",
			}, errors.New(
				"Rate limit exceeded",
			)
	}

	notificationsReceived.Inc()
	log.Printf("Received notification request for user: %s", req.UserId)

	// Insert notification into db
	notificationID, err := s.db.InsertNotification(ctx, req.UserId, req.Message, "pending")
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
		Type:           req.Type,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return &pb.NotificationResponse{Success: false, Error: err.Error()}, err
	}

	// Publish the payload to RabbitMQ
	err = s.producer.Publish("notification_exchange_topic", req.Type, string(data))
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
