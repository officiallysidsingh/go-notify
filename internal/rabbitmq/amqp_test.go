package rabbitmq_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/officiallysidsingh/go-notify/internal/rabbitmq"
)

func TestDialReportsUnreachableBroker(t *testing.T) {
	conn, err := rabbitmq.Dial("amqp://guest:guest@127.0.0.1:1/")

	assert.Nil(t, conn)
	require.Error(t, err)
}
