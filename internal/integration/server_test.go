//go:build integration

package integration_test

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection/grpc_reflection_v1"
	"google.golang.org/grpc/status"

	pb "github.com/officiallysidsingh/go-notify/api/generated"
)

func TestRequestWithoutAPIKeyIsRejected(t *testing.T) {
	s := newStack(t)
	s.startServer(t)
	client := pb.NewNotificationServiceClient(s.dial(t, false))

	_, err := client.SendNotification(context.Background(), &pb.NotificationRequest{
		UserId: "user-1", Message: "hi", Type: "push",
	})

	assert.Equal(t, codes.Unauthenticated, status.Code(err))
}

func TestHealthCheckNeedsNoAPIKey(t *testing.T) {
	s := newStack(t)
	s.startServer(t)

	got, err := s.healthStatus(s.dial(t, false))

	require.NoError(t, err)
	assert.Equal(t, grpc_health_v1.HealthCheckResponse_SERVING, got)
}

func TestRateLimitIsEnforcedAcrossRequests(t *testing.T) {
	s := newStack(t)
	s.cfg.Redis.Limit = 2
	s.startServer(t)

	s.send(t)
	s.send(t)
	_, err := s.serverClient.SendNotification(context.Background(), &pb.NotificationRequest{
		UserId: "user-1", Title: "hello", Message: "world", Type: "push",
	})

	assert.Equal(t, codes.ResourceExhausted, status.Code(err))
}

func TestReflectionIsOffByDefault(t *testing.T) {
	s := newStack(t)
	s.startServer(t)
	client := grpc_reflection_v1.NewServerReflectionClient(s.dial(t, false))

	stream, err := client.ServerReflectionInfo(context.Background())
	if err == nil {
		// The server may reject the stream before the send completes; Send
		// then reports io.EOF and Recv carries the real status.
		sendErr := stream.Send(&grpc_reflection_v1.ServerReflectionRequest{
			MessageRequest: &grpc_reflection_v1.ServerReflectionRequest_ListServices{},
		})
		require.True(t, sendErr == nil || errors.Is(sendErr, io.EOF), "send: %v", sendErr)
		_, err = stream.Recv()
	}

	assert.Equal(t, codes.Unimplemented, status.Code(err))
}

func TestMetricsEndpointExposesRequestCounter(t *testing.T) {
	s := newStack(t)
	s.startServer(t)
	s.send(t)

	body := httpGetBody(t, "http://127.0.0.1"+s.cfg.Metrics.Port+"/metrics")

	assert.Contains(t, body, `gonotify_grpc_requests_total{caller="test-client",code="OK",method="/notify.NotificationService/SendNotification"} 1`)
	assert.Contains(t, body, "notifications_received_total 1")
}
