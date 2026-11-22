package grpc

import (
	"fmt"
	"strings"
	"unicode"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/officiallysidsingh/go-notify/api/generated"
	"github.com/officiallysidsingh/go-notify/internal/notification"
)

const (
	maxUserIDBytes  = 255
	maxMessageBytes = 4096
	maxTitleBytes   = 250
)

func invalid(msg string) error {
	return status.Error(codes.InvalidArgument, msg)
}

func hasControlChars(s string) bool {
	return strings.IndexFunc(s, unicode.IsControl) >= 0
}

func validateSend(req *pb.NotificationRequest) error {
	if req.UserId == "" {
		return invalid("user_id is required")
	}
	if len(req.UserId) > maxUserIDBytes {
		return invalid("user_id exceeds 255 bytes")
	}
	if hasControlChars(req.UserId) {
		return invalid("user_id contains control characters")
	}
	if req.Message == "" {
		return invalid("message is required")
	}
	if len(req.Message) > maxMessageBytes {
		return invalid("message exceeds 4096 bytes")
	}
	if len(req.Title) > maxTitleBytes {
		return invalid("title exceeds 250 bytes")
	}
	if hasControlChars(req.Title) {
		return invalid("title contains control characters")
	}
	switch req.Priority {
	case "", "1", "2", "3", "4", "5":
	default:
		return invalid(fmt.Sprintf("priority must be empty or one of 1..5, got %q", req.Priority))
	}
	if !notification.IsSupportedType(req.Type) {
		return invalid(fmt.Sprintf(
			"type %q is unsupported (supported: %v)", req.Type, notification.SupportedTypes(),
		))
	}
	return nil
}

func validateStatus(req *pb.StatusRequest) error {
	if req.NotificationId <= 0 {
		return invalid("notification_id must be positive")
	}
	return nil
}
