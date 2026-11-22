package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func loadExampleConfig(t *testing.T) *Config {
	t.Helper()
	example, err := os.ReadFile("config.example.yaml")
	require.NoError(t, err)
	configDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.yaml"), example, 0o600))
	cfg, err := Load(configDir)
	require.NoError(t, err)
	return cfg
}

func TestExampleConfigDefinesPostgresDurations(t *testing.T) {
	postgres := loadExampleConfig(t).Postgres

	assert.Equal(t, 20*time.Minute, postgres.ConnMaxLifetime)
	assert.Equal(t, 5*time.Minute, postgres.ConnMaxIdleTime)
	assert.Equal(t, 3*time.Second, postgres.ConnTimeout)
}

func TestLoadAppliesDefaults(t *testing.T) {
	cfg, err := Load(t.TempDir())
	require.NoError(t, err)

	assert.Equal(t, "https://ntfy.sh", cfg.Ntfy.BaseURL)
	assert.Equal(t, 5*time.Minute, cfg.Sweep.StaleAfter)
	assert.Equal(t, 10, cfg.Worker.Concurrency)
	assert.False(t, cfg.GRPC.Reflection)
	assert.Equal(t, defaultPostgresConnTimeout, cfg.Postgres.ConnTimeout)
}

func TestLoadReadsEnvironmentOverrides(t *testing.T) {
	t.Setenv("AUTH_APIKEYS", "billing:k1")

	cfg, err := Load(t.TempDir())
	require.NoError(t, err)

	assert.Equal(t, "billing:k1", cfg.Auth.APIKeys)
}

func TestLoadRejectsMalformedFile(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("grpc: ["), 0o600))

	_, err := Load(dir)

	assert.Error(t, err)
}

func TestExampleConfigIsValidForServerAndWorker(t *testing.T) {
	cfg := loadExampleConfig(t)

	assert.NoError(t, cfg.ValidateServer())
	assert.NoError(t, cfg.ValidateWorker())
}
