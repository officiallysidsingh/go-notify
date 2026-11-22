package rabbitmq_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/officiallysidsingh/go-notify/internal/notification"
	"github.com/officiallysidsingh/go-notify/internal/rabbitmq"
	"github.com/officiallysidsingh/go-notify/internal/rabbitmq/rabbitmqtest"
)

func TestDeclareTopologyDeclaresQueuePerNotificationType(t *testing.T) {
	ch := rabbitmqtest.NewChannel()
	require.NoError(t, rabbitmq.DeclareTopology(ch))

	var names []string
	for _, q := range ch.DeclaredQueues {
		names = append(names, q.Name)
	}
	require.ElementsMatch(t, []string{"queue_email", "queue_sms", "queue_push", "dead_letter_queue"}, names)
}

func TestDeclareTopologyDeadLettersNotificationQueues(t *testing.T) {
	ch := rabbitmqtest.NewChannel()
	require.NoError(t, rabbitmq.DeclareTopology(ch))

	checked := 0
	for _, q := range ch.DeclaredQueues {
		if q.Name == rabbitmq.DeadLetterQueue {
			continue
		}
		require.Equal(t, rabbitmq.DeadLetterExchange, q.Args["x-dead-letter-exchange"], q.Name)
		checked++
	}
	require.Equal(t, len(notification.Types()), checked)
}

func TestDeclareTopologyStopsAtFirstFailure(t *testing.T) {
	cause := errors.New("boom")
	fail := func(call int) error {
		if call == 1 {
			return cause
		}
		return nil
	}
	tests := []struct {
		name    string
		script  func(*rabbitmqtest.Channel)
		wantOps int
	}{
		{"first exchange", func(c *rabbitmqtest.Channel) { c.ExchangeDeclareErr = fail }, 0},
		{"first queue", func(c *rabbitmqtest.Channel) { c.QueueDeclareErr = fail }, 1},
		{"first bind", func(c *rabbitmqtest.Channel) { c.QueueBindErr = fail }, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch := rabbitmqtest.NewChannel()
			tt.script(ch)
			err := rabbitmq.DeclareTopology(ch)
			require.ErrorIs(t, err, cause)
			require.Equal(t, tt.wantOps, ch.TotalOps())
		})
	}
}
