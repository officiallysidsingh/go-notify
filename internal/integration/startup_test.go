//go:build integration

package integration_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
	"google.golang.org/grpc/reflection/grpc_reflection_v1"

	pb "github.com/officiallysidsingh/go-notify/api/generated"
	"github.com/officiallysidsingh/go-notify/internal/app"
)

// occupy holds a TCP port so the process under test cannot listen on it.
func occupy(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", ":0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	_, port, err := net.SplitHostPort(ln.Addr().String())
	require.NoError(t, err)
	return ":" + port
}

func TestRunServerFailsWhenGRPCPortIsTaken(t *testing.T) {
	s := newStack(t)
	s.cfg.GRPC.Port = occupy(t)

	err := app.RunServer(context.Background(), s.cfg, zaptest.NewLogger(t))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "listen on "+s.cfg.GRPC.Port)
}

func TestRunServerStopsWhenMetricsPortIsTaken(t *testing.T) {
	s := newStack(t)
	s.cfg.Metrics.Port = occupy(t)

	err := app.RunServer(context.Background(), s.cfg, zaptest.NewLogger(t))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "metrics server on "+s.cfg.Metrics.Port)
}

func TestRunServerFailsWhenBrokerIsUnreachable(t *testing.T) {
	s := newStack(t)
	s.cfg.RabbitMQ.URL = "amqp://guest:guest@127.0.0.1:1/"

	err := app.RunServer(context.Background(), s.cfg, zaptest.NewLogger(t))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "rabbitmq")
}

func TestRunWorkerStopsWhenMetricsPortIsTaken(t *testing.T) {
	s := newStack(t)
	s.cfg.Worker.MetricsPort = occupy(t)

	err := app.RunWorker(context.Background(), s.cfg, zaptest.NewLogger(t))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "worker HTTP server on "+s.cfg.Worker.MetricsPort)
}

func TestDisabledAuthAcceptsAnonymousCalls(t *testing.T) {
	s := newStack(t)
	s.cfg.Auth.Disabled = true
	s.startServer(t)
	client := pb.NewNotificationServiceClient(s.dial(t, false))

	res, err := client.SendNotification(context.Background(), &pb.NotificationRequest{
		UserId: "user-1", Title: "hello", Message: "world", Type: "push",
	})

	require.NoError(t, err)
	assert.True(t, res.Success)
}

func TestReflectionListsServicesWhenEnabled(t *testing.T) {
	s := newStack(t)
	s.cfg.Auth.Disabled = true // the reflection stream carries no API key
	s.cfg.GRPC.Reflection = true
	s.startServer(t)
	client := grpc_reflection_v1.NewServerReflectionClient(s.dial(t, false))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stream, err := client.ServerReflectionInfo(ctx)
	require.NoError(t, err)
	require.NoError(t, stream.Send(&grpc_reflection_v1.ServerReflectionRequest{
		MessageRequest: &grpc_reflection_v1.ServerReflectionRequest_ListServices{},
	}))
	res, err := stream.Recv()

	require.NoError(t, err)
	var names []string
	for _, svc := range res.GetListServicesResponse().GetService() {
		names = append(names, svc.GetName())
	}
	assert.Contains(t, names, "notify.NotificationService")
}
