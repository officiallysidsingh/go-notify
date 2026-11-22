package app_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/officiallysidsingh/go-notify/config"
	"github.com/officiallysidsingh/go-notify/internal/app"
)

const unreachableDSN = "postgres://notify:notify_pass@127.0.0.1:1/go_notify?sslmode=disable"

func validServerConfig() *config.Config {
	return &config.Config{
		GRPC:     config.GRPCConfig{Port: ":0"},
		Auth:     config.AuthConfig{APIKeys: "billing:key-1"},
		RabbitMQ: config.RabbitMQConfig{URL: "amqp://guest:guest@127.0.0.1:1/"},
		Postgres: config.PostgresConfig{
			DataSourceName: unreachableDSN,
			ConnTimeout:    100 * time.Millisecond,
		},
		Redis:   config.RedisConfig{Addr: "127.0.0.1:1", Limit: 10, Window: time.Minute},
		Metrics: config.MetricsConfig{Port: ":0"},
		Sweep: config.SweepConfig{
			Interval: time.Minute, StaleAfter: time.Minute, BatchSize: 10, MaxAttempts: 3,
		},
		Health: config.HealthConfig{Interval: time.Second},
	}
}

func validWorkerConfig() *config.Config {
	return &config.Config{
		RabbitMQ: config.RabbitMQConfig{URL: "amqp://guest:guest@127.0.0.1:1/"},
		Postgres: config.PostgresConfig{
			DataSourceName: unreachableDSN,
			ConnTimeout:    100 * time.Millisecond,
		},
		Ntfy: config.NtfyConfig{BaseURL: "https://ntfy.sh", Topic: "t", Timeout: time.Second},
		Worker: config.WorkerConfig{
			Concurrency: 1, SendAttempts: 1, RetryDelay: time.Millisecond, MetricsPort: ":0",
		},
	}
}

func TestRunServerFailsWhenDatabaseIsUnreachable(t *testing.T) {
	cfg := validServerConfig()

	err := app.RunServer(context.Background(), cfg, zaptest.NewLogger(t))

	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "postgres")
}

func TestRunServerFailsOnInvalidAPIKeys(t *testing.T) {
	cfg := validServerConfig()
	cfg.Auth.APIKeys = "broken-entry-missing-colon"

	err := app.RunServer(context.Background(), cfg, zaptest.NewLogger(t))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "auth.apiKeys")
}

func TestRunWorkerFailsWhenDatabaseIsUnreachable(t *testing.T) {
	cfg := validWorkerConfig()

	err := app.RunWorker(context.Background(), cfg, zaptest.NewLogger(t))

	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "postgres")
}
