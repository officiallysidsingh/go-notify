package logging

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"
)

func TestNewEnablesConfiguredLevel(t *testing.T) {
	logger, err := New("warn")
	require.NoError(t, err)
	assert.False(t, logger.Core().Enabled(zapcore.InfoLevel))
	assert.True(t, logger.Core().Enabled(zapcore.WarnLevel))
}

func TestNewRejectsUnknownLevel(t *testing.T) {
	logger, err := New("loud")
	assert.Nil(t, logger)
	assert.ErrorContains(t, err, "loud")
}
