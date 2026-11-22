//go:build integration

package integration_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/officiallysidsingh/go-notify/internal/rabbitmq"
)

func TestDialOpensChannelsUntilConnectionCloses(t *testing.T) {
	conn, err := rabbitmq.Dial(testRabbitmqURL)
	require.NoError(t, err)

	ch, err := conn.Channel()
	require.NoError(t, err)
	require.NoError(t, ch.Close())
	assert.False(t, conn.IsClosed())

	require.NoError(t, conn.Close())

	ch, err = conn.Channel()
	assert.Nil(t, ch)
	assert.Error(t, err)
}
