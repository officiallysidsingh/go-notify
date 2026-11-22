package grpc

import (
	"context"
	"errors"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/officiallysidsingh/go-notify/api/generated"
	"github.com/officiallysidsingh/go-notify/internal/auth"
	"github.com/officiallysidsingh/go-notify/internal/metrics"
	"github.com/officiallysidsingh/go-notify/internal/notification"
	"github.com/officiallysidsingh/go-notify/internal/rabbitmq"
	"github.com/officiallysidsingh/go-notify/internal/repository"
)

type Publisher interface {
	Publish(ctx context.Context, exchange, routingKey string, body []byte) error
}

type NotificationStore interface {
	InsertNotification(ctx context.Context, message notification.Message) (int64, error)
	MarkQueued(ctx context.Context, id int64) error
	UpdateNotificationStatus(ctx context.Context, id int64, status string) error
	GetNotificationStatus(ctx context.Context, id int64) (string, error)
}

type RateLimiter interface {
	Allow(ctx context.Context, key string) (bool, error)
}

const statusUpdateTimeout = 5 * time.Second

type Dependencies struct {
	Publisher   Publisher
	Store       NotificationStore
	RateLimiter RateLimiter
	Metrics     *metrics.Server
	Logger      *zap.Logger
}

type NotificationServer struct {
	pb.UnimplementedNotificationServiceServer
	deps Dependencies
}

func NewNotificationServer(deps Dependencies) *NotificationServer {
	return &NotificationServer{deps: deps}
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
	s.deps.Metrics.Received.Inc()

	notificationID, err := s.storeNotification(ctx, req)
	if err != nil {
		return nil, err
	}
	if err := s.enqueueNotification(ctx, notificationID, req); err != nil {
		return nil, err
	}
	s.markQueued(ctx, notificationID)

	return &pb.NotificationResponse{Success: true, NotificationId: notificationID}, nil
}

func (s *NotificationServer) checkRateLimit(ctx context.Context, userID string) error {
	allowed, err := s.deps.RateLimiter.Allow(ctx, userID)
	if err != nil {
		s.deps.Logger.Warn("rate limiter unavailable",
			zap.String("caller", auth.CallerFromContext(ctx)),
			zap.Error(err),
		)
		return status.Error(codes.Unavailable, "rate limiter unavailable")
	}
	if !allowed {
		s.deps.Metrics.RateLimited.Inc()
		return status.Error(codes.ResourceExhausted, "rate limit exceeded")
	}
	return nil
}

func (s *NotificationServer) storeNotification(
	ctx context.Context,
	req *pb.NotificationRequest,
) (int64, error) {
	notificationID, err := s.deps.Store.InsertNotification(ctx, messageFor(0, req))
	if err != nil {
		s.deps.Logger.Error("failed to store notification", zap.Error(err))
		return 0, status.Error(codes.Internal, "failed to store notification")
	}
	return notificationID, nil
}

func (s *NotificationServer) enqueueNotification(
	ctx context.Context,
	notificationID int64,
	req *pb.NotificationRequest,
) error {
	err := s.publish(ctx, notificationID, req)
	if err == nil {
		return nil
	}
	s.deps.Logger.Error("failed to enqueue notification",
		zap.Int64("notification_id", notificationID),
		zap.Error(err),
	)
	s.markFailed(notificationID)
	return status.Error(codes.Unavailable, "failed to enqueue notification")
}

func (s *NotificationServer) publish(
	ctx context.Context,
	notificationID int64,
	req *pb.NotificationRequest,
) error {
	body := messageFor(notificationID, req).JSON()
	return s.deps.Publisher.Publish(ctx, rabbitmq.NotificationExchange, req.Type, body)
}

// A failure here is not fatal: the notification stays pending and the sweep
// republishes it.
func (s *NotificationServer) markQueued(ctx context.Context, notificationID int64) {
	if err := s.deps.Store.MarkQueued(ctx, notificationID); err != nil {
		s.deps.Logger.Warn("failed to mark notification queued",
			zap.Int64("notification_id", notificationID),
			zap.Error(err),
		)
	}
}

// The request context may already be cancelled when enqueueing fails, so the
// status update runs on its own context.
func (s *NotificationServer) markFailed(notificationID int64) {
	ctx, cancel := context.WithTimeout(context.Background(), statusUpdateTimeout)
	defer cancel()

	err := s.deps.Store.UpdateNotificationStatus(ctx, notificationID, notification.StatusFailed)
	if err != nil {
		s.deps.Logger.Error("failed to mark notification failed",
			zap.Int64("notification_id", notificationID),
			zap.Error(err),
		)
	}
}

func messageFor(notificationID int64, req *pb.NotificationRequest) notification.Message {
	return notification.Message{
		NotificationID: notificationID,
		UserID:         req.UserId,
		Title:          req.Title,
		Priority:       req.Priority,
		Message:        req.Message,
		Type:           req.Type,
	}
}

func (s *NotificationServer) GetNotificationStatus(
	ctx context.Context,
	req *pb.StatusRequest,
) (*pb.StatusResponse, error) {
	if err := validateStatus(req); err != nil {
		return nil, err
	}
	storedStatus, err := s.deps.Store.GetNotificationStatus(ctx, req.NotificationId)
	if errors.Is(err, repository.ErrNotificationNotFound) {
		return nil, status.Errorf(codes.NotFound, "notification %d not found", req.NotificationId)
	}
	if err != nil {
		s.deps.Logger.Error("failed to get notification status",
			zap.Int64("notification_id", req.NotificationId),
			zap.Error(err),
		)
		return nil, status.Error(codes.Internal, "failed to get notification status")
	}
	return &pb.StatusResponse{Status: storedStatus}, nil
}
