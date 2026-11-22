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
	LoadConfig(configDir)
	return AppConfig
}

func TestExampleConfigDefinesPostgresDurations(t *testing.T) {
	postgres := loadExampleConfig(t).Postgres

	assert.Equal(t, 20*time.Minute, postgres.ConnMaxLifetime)
	assert.Equal(t, 5*time.Minute, postgres.ConnMaxIdleTime)
	assert.Equal(t, 3*time.Second, postgres.ConnTimeout)
}

func TestLoadConfigDefaultsPostgresConnTimeout(t *testing.T) {
	LoadConfig(t.TempDir())

	assert.Equal(t, defaultPostgresConnTimeout, AppConfig.Postgres.ConnTimeout)
}
