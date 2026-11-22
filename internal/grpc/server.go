package grpc

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/officiallysidsingh/go-notify/api/generated"
	"github.com/officiallysidsingh/go-notify/internal/notification"
	"github.com/officiallysidsingh/go-notify/internal/rabbitmq"
	"github.com/officiallysidsingh/go-notify/internal/repository"
)

type Publisher interface {
	Publish(ctx context.Context, exchange, routingKey string, body []byte) error
}

type NotificationStore interface {
	InsertNotification(ctx context.Context, message notification.Message) (int64, error)
	UpdateNotificationStatus(ctx context.Context, id int64, status string) error
	GetNotificationStatus(ctx context.Context, id int64) (string, error)
}

type RateLimiter interface {
	Allow(ctx context.Context, key string) (bool, error)
}

const statusUpdateTimeout = 5 * time.Second

var notificationsReceived = prometheus.NewCounter(
	prometheus.CounterOpts{
		Name: "notifications_received_total",
		Help: "Total number of notifications received via gRPC",
	},
)

func init() {
	prometheus.MustRegister(notificationsReceived)
}

type NotificationServer struct {
	pb.UnimplementedNotificationServiceServer
	publisher   Publisher
	store       NotificationStore
	rateLimiter RateLimiter
}

func NewNotificationServer(
	publisher Publisher,
	store NotificationStore,
	limiter RateLimiter,
) *NotificationServer {
	return &NotificationServer{
		publisher:   publisher,
		store:       store,
		rateLimiter: limiter,
	}
}

func (s *NotificationServer) SendNotification(
	ctx context.Context,
	req *pb.NotificationRequest,
) (*pb.NotificationResponse, error) {
	if err := validateSend(req); err != nil {
		return nil, err
	}
	if err := s.checkRateLimit(ctx, req.UserId); err != nil {
		return nil, err
	}
	notificationsReceived.Inc()

	notificationID, err := s.store.InsertNotification(
		ctx, notification.Message{
			UserID:   req.UserId,
			Title:    req.Title,
			Priority: req.Priority,
			Message:  req.Message,
			Type:     req.Type,
		},
	)
	if err != nil {
		log.Printf("Failed to store notification for user %s: %v", req.UserId, err)
		return nil, status.Error(codes.Internal, "failed to store notification")
	}

	if err := s.enqueue(ctx, notificationID, req); err != nil {
		log.Printf("Failed to enqueue notification %d: %v", notificationID, err)
		s.markFailed(notificationID)
		return nil, status.Error(codes.Unavailable, "failed to enqueue notification")
	}

	return &pb.NotificationResponse{Success: true, NotificationId: notificationID}, nil
}

func (s *NotificationServer) checkRateLimit(ctx context.Context, userID string) error {
	allowed, err := s.rateLimiter.Allow(ctx, userID)
	if err != nil {
		log.Printf("Rate limiter error for user %s: %v", userID, err)
		return status.Error(codes.Unavailable, "rate limiter unavailable")
	}
	if !allowed {
		return status.Error(codes.ResourceExhausted, "rate limit exceeded")
	}
	return nil
}

func (s *NotificationServer) enqueue(
	ctx context.Context,
	notificationID int64,
	req *pb.NotificationRequest,
) error {
	body, err := json.Marshal(notification.Message{
		NotificationID: notificationID,
		UserID:         req.UserId,
		Title:          req.Title,
		Priority:       req.Priority,
		Message:        req.Message,
		Type:           req.Type,
	})
	if err != nil {
		return err
	}
	return s.publisher.Publish(ctx, rabbitmq.NotificationExchange, req.Type, body)
}

// The request context may already be cancelled when enqueueing fails, so the
// status update runs on its own context.
func (s *NotificationServer) markFailed(notificationID int64) {
	ctx, cancel := context.WithTimeout(context.Background(), statusUpdateTimeout)
	defer cancel()

	err := s.store.UpdateNotificationStatus(ctx, notificationID, notification.StatusFailed)
	if err != nil {
		log.Printf("Failed to mark notification %d as failed: %v", notificationID, err)
	}
}

func (s *NotificationServer) GetNotificationStatus(
	ctx context.Context,
	req *pb.StatusRequest,
) (*pb.StatusResponse, error) {
	storedStatus, err := s.store.GetNotificationStatus(ctx, req.NotificationId)
	if errors.Is(err, repository.ErrNotificationNotFound) {
		return nil, status.Errorf(codes.NotFound, "notification %d not found", req.NotificationId)
	}
	if err != nil {
		log.Printf("Failed to get status of notification %d: %v", req.NotificationId, err)
		return nil, status.Error(codes.Internal, "failed to get notification status")
	}
	return &pb.StatusResponse{Status: storedStatus}, nil
}
