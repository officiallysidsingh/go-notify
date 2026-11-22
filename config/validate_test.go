package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func validServer() *Config {
	return &Config{
		GRPC:     GRPCConfig{Port: ":50051"},
		Auth:     AuthConfig{APIKeys: "svc:key"},
		RabbitMQ: RabbitMQConfig{URL: "amqp://u:p@host:5672/"},
		Postgres: PostgresConfig{DataSourceName: "postgres://x"},
		Redis:    RedisConfig{Addr: "redis:6379", Limit: 5, Window: 60_000_000_000},
		Metrics:  MetricsConfig{Port: ":9091"},
		Sweep:    SweepConfig{Interval: 1, StaleAfter: 1, BatchSize: 1, MaxAttempts: 1},
		Health:   HealthConfig{Interval: 1},
	}
}

func validWorker() *Config {
	return &Config{
		RabbitMQ: RabbitMQConfig{URL: "amqps://u:p@host:5671/"},
		Postgres: PostgresConfig{DataSourceName: "postgres://x"},
		Ntfy:     NtfyConfig{BaseURL: "https://ntfy.sh", Topic: "t", Timeout: 1},
		Worker:   WorkerConfig{Concurrency: 1, SendAttempts: 1, RetryDelay: 1, MetricsPort: ":9092"},
	}
}

func TestValidateServerAcceptsValidConfig(t *testing.T) {
	assert.NoError(t, validServer().ValidateServer())
}

func TestValidateServerReportsEveryProblem(t *testing.T) {
	err := (&Config{}).ValidateServer()

	assert.ErrorContains(t, err, "grpc.port")
	assert.ErrorContains(t, err, "rabbitmq.url")
	assert.ErrorContains(t, err, "auth.apiKeys")
}

func TestValidateServerAllowsMissingKeysWhenAuthDisabled(t *testing.T) {
	cfg := validServer()
	cfg.Auth = AuthConfig{Disabled: true}

	assert.NoError(t, cfg.ValidateServer())
}

func TestValidateServerRejectsNonAMQPURL(t *testing.T) {
	cfg := validServer()
	cfg.RabbitMQ.URL = "http://x"

	assert.ErrorContains(t, cfg.ValidateServer(), "rabbitmq.url")
}

func TestValidateWorkerAcceptsValidConfig(t *testing.T) {
	assert.NoError(t, validWorker().ValidateWorker())
}

func TestValidateWorkerRequiresNtfyTopic(t *testing.T) {
	cfg := validWorker()
	cfg.Ntfy.Topic = ""

	assert.ErrorContains(t, cfg.ValidateWorker(), "ntfy.topic")
}

func TestValidateWorkerRejectsNonPositiveConcurrency(t *testing.T) {
	cfg := validWorker()
	cfg.Worker.Concurrency = 0

	assert.ErrorContains(t, cfg.ValidateWorker(), "worker.concurrency")
}

func TestValidateWorkerRejectsNonHTTPNtfyURL(t *testing.T) {
	cfg := validWorker()
	cfg.Ntfy.BaseURL = "ftp://x"

	assert.ErrorContains(t, cfg.ValidateWorker(), "ntfy.baseURL")
}

func TestValidateServerRejectsUnparseableURL(t *testing.T) {
	cfg := validServer()
	cfg.RabbitMQ.URL = "://bad"

	assert.ErrorContains(t, cfg.ValidateServer(), "rabbitmq.url: is not a valid URL")
}
