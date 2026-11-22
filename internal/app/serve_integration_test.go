//go:build integration

package app

import (
	"context"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/officiallysidsingh/go-notify/config"
)

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func TestStartServerGoroutinesReportsGRPCServeFailure(t *testing.T) {
	cfg := &config.Config{
		GRPC:     config.GRPCConfig{Port: ":0"},
		Auth:     config.AuthConfig{Disabled: true},
		RabbitMQ: config.RabbitMQConfig{URL: envOr("TEST_RABBITMQ_URL", "amqp://guest:guest@localhost:55672/")},
		Postgres: config.PostgresConfig{
			DataSourceName: envOr("TEST_POSTGRES_DSN", "postgres://go_notify_test:go_notify_test@localhost:55432/go_notify_test?sslmode=disable"),
			ConnTimeout:    3 * time.Second,
		},
		Redis:   config.RedisConfig{Addr: envOr("TEST_REDIS_ADDR", "localhost:56379"), Limit: 1, Window: time.Minute},
		Metrics: config.MetricsConfig{Port: "127.0.0.1:0"},
		Sweep:   config.SweepConfig{Interval: time.Hour, StaleAfter: time.Hour, BatchSize: 1, MaxAttempts: 1},
		Health:  config.HealthConfig{Interval: time.Hour},
	}
	logger := zap.NewNop()
	deps, err := buildServerDeps(cfg, logger)
	require.NoError(t, err)

	// Serving on a listener that is already closed makes Serve fail at once.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	require.NoError(t, listener.Close())

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	httpServer, serveErr := startServerGoroutines(ctx, &wg, cfg, logger, deps, listener)

	select {
	case err := <-serveErr:
		assert.ErrorContains(t, err, "gRPC serve")
	case <-time.After(10 * time.Second):
		t.Fatal("no serve error reported")
	}

	cancel()
	wg.Wait()
	shutdownServer(deps, httpServer)
}
