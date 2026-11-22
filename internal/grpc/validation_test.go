package grpc

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/officiallysidsingh/go-notify/api/generated"
	"github.com/officiallysidsingh/go-notify/internal/notification"
)

func goodSend() *pb.NotificationRequest {
	return &pb.NotificationRequest{
		UserId:   "user-1",
		Title:    "Hello",
		Message:  "World",
		Priority: "3",
		Type:     notification.TypePush,
	}
}

func TestValidateSendRejects(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*pb.NotificationRequest)
		want   string
	}{
		{"empty user_id", func(r *pb.NotificationRequest) { r.UserId = "" }, "user_id"},
		{"user_id too long", func(r *pb.NotificationRequest) { r.UserId = strings.Repeat("a", 256) }, "user_id"},
		{"user_id control chars", func(r *pb.NotificationRequest) { r.UserId = "a\tb" }, "user_id"},
		{"empty message", func(r *pb.NotificationRequest) { r.Message = "" }, "message"},
		{"message too long", func(r *pb.NotificationRequest) { r.Message = strings.Repeat("a", 4097) }, "message"},
		{"title too long", func(r *pb.NotificationRequest) { r.Title = strings.Repeat("a", 251) }, "title"},
		{"title newline", func(r *pb.NotificationRequest) { r.Title = "a\nb" }, "title"},
		{"priority 6", func(r *pb.NotificationRequest) { r.Priority = "6" }, "priority"},
		{"priority high", func(r *pb.NotificationRequest) { r.Priority = "high" }, "priority"},
		{"type email", func(r *pb.NotificationRequest) { r.Type = "email" }, "type"},
		{"type sms", func(r *pb.NotificationRequest) { r.Type = "sms" }, "type"},
		{"empty type", func(r *pb.NotificationRequest) { r.Type = "" }, "type"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := goodSend()
			tt.mutate(req)

			err := validateSend(req)

			require.Error(t, err)
			assert.Equal(t, codes.InvalidArgument, status.Code(err))
			assert.ErrorContains(t, err, tt.want)
		})
	}
}

func TestValidateSendAcceptsLimits(t *testing.T) {
	req := goodSend()
	req.UserId = strings.Repeat("u", 255)
	req.Message = strings.Repeat("m", 4096)
	req.Title = strings.Repeat("t", 250)
	req.Priority = "5"
	assert.NoError(t, validateSend(req))

	req = goodSend()
	req.Title = "Größe"
	assert.NoError(t, validateSend(req))
}

func TestValidateSendCountsBytesNotCharacters(t *testing.T) {
	req := goodSend()
	req.Message = strings.Repeat("é", 2000)
	assert.NoError(t, validateSend(req))

	req.Message = strings.Repeat("é", 2049)
	assert.ErrorContains(t, validateSend(req), "message")
}

func TestValidateSendNamesSupportedTypes(t *testing.T) {
	req := goodSend()
	req.Type = "email"

	assert.ErrorContains(t, validateSend(req), "push")
}

func TestValidateStatusRejectsNonPositiveID(t *testing.T) {
	for _, id := range []int64{0, -1} {
		err := validateStatus(&pb.StatusRequest{NotificationId: id})

		assert.Equal(t, codes.InvalidArgument, status.Code(err))
		assert.ErrorContains(t, err, "notification_id")
	}
	assert.NoError(t, validateStatus(&pb.StatusRequest{NotificationId: 1}))
}
