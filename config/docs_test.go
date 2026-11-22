package config

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// documentedKeys lists every key Load reads (dotted, as written in the YAML).
func documentedKeys() []string {
	return []string{
		"grpc.port", "grpc.reflection",
		"auth.apiKeys", "auth.disabled",
		"logging.level",
		"ntfy.baseURL", "ntfy.topic", "ntfy.token", "ntfy.timeout",
		"rabbitmq.url",
		"postgres.DataSourceName", "postgres.MaxOpenConns", "postgres.MaxIdleConns",
		"postgres.ConnMaxLifetime", "postgres.ConnMaxIdleTime", "postgres.ConnTimeout",
		"redis.addr", "redis.limit", "redis.window",
		"metrics.port",
		"worker.concurrency", "worker.sendAttempts", "worker.retryDelay", "worker.metricsPort",
		"sweep.interval", "sweep.staleAfter", "sweep.batchSize", "sweep.maxAttempts",
		"health.interval",
	}
}

func TestEveryConfigKeyIsDocumented(t *testing.T) {
	body, err := os.ReadFile("../docs/configuration.md")
	require.NoError(t, err)
	text := string(body)
	for _, key := range documentedKeys() {
		assert.Contains(t, text, "`"+key+"`", "key %s missing from docs/configuration.md", key)
	}
}

func TestExampleConfigListsEveryKey(t *testing.T) {
	body, err := os.ReadFile("config.example.yaml")
	require.NoError(t, err)
	text := strings.ToLower(string(body))
	for _, key := range documentedKeys() {
		parts := strings.Split(strings.ToLower(key), ".")
		last := parts[len(parts)-1]
		assert.Contains(t, text, last+":", "key %s missing from config.example.yaml (looked for %q)", key, last+":")
	}
}
