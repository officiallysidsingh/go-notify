package producer

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/streadway/amqp"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/officiallysidsingh/go-notify/internal/rabbitmq/rabbitmqtest"
)

const (
	testDelay   = time.Millisecond
	testConfirm = 20 * time.Millisecond
)

func boolPtr(b bool) *bool { return &b }

// confirmingConn returns a connection whose channels answer every publish
// with the given ack value, optionally failing publishes via publishErr.
func confirmingConn(ack bool, publishErr rabbitmqtest.ErrFunc) *rabbitmqtest.Connection {
	conn := rabbitmqtest.NewConnection()
	conn.ChannelFunc = func(int) (*rabbitmqtest.Channel, error) {
		ch := rabbitmqtest.NewChannel()
		ch.AutoConfirm = boolPtr(ack)
		ch.PublishErr = publishErr
		return ch, nil
	}
	return conn
}

func build(t *testing.T, d *rabbitmqtest.Dialer) *RabbitMQProducer {
	t.Helper()
	p, err := newProducer("amqp://test", d.Dialer(), zap.NewNop(), testDelay, testDelay, testConfirm)
	require.NoError(t, err)
	return p
}

func TestPublishReturnsAfterBrokerConfirms(t *testing.T) {
	conn := rabbitmqtest.NewConnection()
	p := build(t, rabbitmqtest.NewDialer(conn))

	conn.Channels[0].ConfirmNext(true)
	require.NoError(t, p.Publish(context.Background(), "ex", "key", []byte("{}")))
}

func TestPublishSendsPersistentJSON(t *testing.T) {
	conn := confirmingConn(true, nil)
	p := build(t, rabbitmqtest.NewDialer(conn))

	require.NoError(t, p.Publish(context.Background(), "ex", "push", []byte(`{"a":1}`)))

	ch := conn.Channels[0]
	require.Len(t, ch.Published, 1)
	require.Equal(t, "application/json", ch.Published[0].ContentType)
	require.Equal(t, uint8(amqp.Persistent), ch.Published[0].DeliveryMode)
	require.Equal(t, []byte(`{"a":1}`), ch.Published[0].Body)
	require.Equal(t, []string{"push"}, ch.PublishedRoutingKeys)
	require.True(t, ch.ConfirmEnabled)
}

func TestPublishReconnectsAfterChannelFailure(t *testing.T) {
	first := confirmingConn(true, func(call int) error {
		if call == 1 {
			return amqp.ErrClosed
		}
		return nil
	})
	second := confirmingConn(true, nil)
	d := rabbitmqtest.NewDialer(first, second)
	p := build(t, d)

	require.NoError(t, p.Publish(context.Background(), "ex", "k", []byte("x")))
	require.Equal(t, 2, d.DialCount())
	require.Equal(t, 1, first.CloseCalls)
	require.Len(t, second.Channels[0].Published, 1)
}

func TestPublishFailsWhenBrokerNacks(t *testing.T) {
	first := confirmingConn(false, nil)
	// Later dials keep nacking too so the retries exhaust, then recover below.
	d := rabbitmqtest.NewDialer(first, confirmingConn(false, nil), confirmingConn(false, nil), confirmingConn(true, nil))
	p := build(t, d)

	err := p.Publish(context.Background(), "ex", "k", []byte("x"))
	require.ErrorIs(t, err, errNotConfirmed)

	// The failed publish dropped the connection; the next one dials again.
	before := d.DialCount()
	require.NoError(t, p.Publish(context.Background(), "ex", "k", []byte("x")))
	require.Greater(t, d.DialCount(), before)
}

func TestPublishFailsWhenConfirmTimesOut(t *testing.T) {
	silent := rabbitmqtest.NewConnection() // never confirms
	d := rabbitmqtest.NewDialer(silent, silent, silent, confirmingConn(true, nil))
	p := build(t, d)

	err := p.Publish(context.Background(), "ex", "k", []byte("x"))
	require.ErrorIs(t, err, errNotConfirmed)

	before := d.DialCount()
	require.NoError(t, p.Publish(context.Background(), "ex", "k", []byte("x")))
	require.Greater(t, d.DialCount(), before)
}

func TestPublishStopsRetryingWhenContextIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	conn := confirmingConn(true, func(int) error {
		cancel()
		return amqp.ErrClosed
	})
	d := rabbitmqtest.NewDialer(conn)
	p, err := newProducer("u", d.Dialer(), zap.NewNop(), testDelay, time.Hour, testConfirm)
	require.NoError(t, err)

	err = p.Publish(ctx, "ex", "k", []byte("x"))
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, d.DialCount())
}

func TestPublishGivesUpAfterThreeAttempts(t *testing.T) {
	cause := errors.New("channel exploded")
	newConn := func() *rabbitmqtest.Connection {
		return confirmingConn(true, func(int) error { return cause })
	}
	d := rabbitmqtest.NewDialer(newConn(), newConn(), newConn(), newConn())
	p := build(t, d)

	err := p.Publish(context.Background(), "ex", "k", []byte("x"))
	require.ErrorIs(t, err, cause)
	require.Equal(t, 3, d.DialCount()) // initial connect + two reconnects
}

func TestNewProducerRetriesStartupConnection(t *testing.T) {
	boom := errors.New("refused")
	d := rabbitmqtest.NewScriptedDialer(
		rabbitmqtest.DialResult{Err: boom},
		rabbitmqtest.DialResult{Err: boom},
		rabbitmqtest.DialResult{Conn: rabbitmqtest.NewConnection()},
	)
	p, err := newProducer("u", d.Dialer(), zap.NewNop(), testDelay, testDelay, testConfirm)
	require.NoError(t, err)
	require.NotNil(t, p)
	require.Equal(t, 3, d.DialCount())
}

func TestNewProducerFailsAfterFiveAttempts(t *testing.T) {
	boom := errors.New("refused")
	d := rabbitmqtest.NewScriptedDialer(rabbitmqtest.DialResult{Err: boom})
	p, err := newProducer("u", d.Dialer(), zap.NewNop(), testDelay, testDelay, testConfirm)
	require.Nil(t, p)
	require.ErrorIs(t, err, boom)
	require.Equal(t, 5, d.DialCount())
}

func TestPingReconnectsClosedConnection(t *testing.T) {
	first, second := rabbitmqtest.NewConnection(), rabbitmqtest.NewConnection()
	d := rabbitmqtest.NewDialer(first, second)
	p := build(t, d)

	first.IsClosedValue = true
	require.NoError(t, p.Ping(context.Background()))
	require.Equal(t, 2, d.DialCount())

	require.NoError(t, p.Ping(context.Background()))
	require.Equal(t, 2, d.DialCount())
}

func TestPingReportsUnreachableBroker(t *testing.T) {
	boom := errors.New("refused")
	conn := rabbitmqtest.NewConnection()
	d := rabbitmqtest.NewScriptedDialer(
		rabbitmqtest.DialResult{Conn: conn},
		rabbitmqtest.DialResult{Err: boom},
	)
	p := build(t, d)

	conn.IsClosedValue = true
	err := p.Ping(context.Background())
	require.ErrorIs(t, err, boom)
}

func TestCloseIsSafeToCallTwice(t *testing.T) {
	p := build(t, rabbitmqtest.NewDialer(rabbitmqtest.NewConnection()))
	require.NotPanics(t, func() {
		p.Close()
		p.Close()
	})
}

func observedLogger() (*zap.Logger, *observer.ObservedLogs) {
	core, logs := observer.New(zap.ErrorLevel)
	return zap.New(core), logs
}

func TestNewProducerToleratesNilLogger(t *testing.T) {
	conn := confirmingConn(true, nil)
	d := rabbitmqtest.NewDialer(conn)
	p, err := newProducer("u", d.Dialer(), nil, testDelay, testDelay, testConfirm)
	require.NoError(t, err)

	require.NotNil(t, p.logger)
	require.NoError(t, p.Publish(context.Background(), "ex", "k", []byte("x")))
}

func TestPingReturnsContextError(t *testing.T) {
	d := rabbitmqtest.NewDialer(rabbitmqtest.NewConnection())
	p := build(t, d)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	require.ErrorIs(t, p.Ping(ctx), context.Canceled)
	require.Equal(t, 1, d.DialCount())
}

func TestPublishReturnsContextErrorBeforeSending(t *testing.T) {
	conn := confirmingConn(true, nil)
	p := build(t, rabbitmqtest.NewDialer(conn))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := p.Publish(ctx, "ex", "k", []byte("x"))

	require.ErrorIs(t, err, context.Canceled)
	require.Empty(t, conn.Channels[0].Published)
}

func TestPublishReportsFailedReconnect(t *testing.T) {
	boom := errors.New("refused")
	conn := rabbitmqtest.NewConnection()
	d := rabbitmqtest.NewScriptedDialer(
		rabbitmqtest.DialResult{Conn: conn},
		rabbitmqtest.DialResult{Err: boom},
	)
	p := build(t, d)
	conn.IsClosedValue = true

	err := p.Publish(context.Background(), "ex", "k", []byte("x"))

	require.ErrorIs(t, err, boom)
	require.Equal(t, 1+publishAttempts, d.DialCount())
}

func TestPublishStopsWaitingForConfirmWhenContextIsCancelled(t *testing.T) {
	silent := rabbitmqtest.NewConnection() // never confirms
	d := rabbitmqtest.NewDialer(silent)
	p, err := newProducer("u", d.Dialer(), zap.NewNop(), testDelay, time.Hour, time.Hour)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(testConfirm, cancel)

	err = p.Publish(ctx, "ex", "k", []byte("x"))

	require.ErrorIs(t, err, context.Canceled)
}

func TestNewProducerClosesConnectionWhenSetupFails(t *testing.T) {
	cause := errors.New("setup failed")
	tests := []struct {
		name    string
		channel func(call int) (*rabbitmqtest.Channel, error)
		want    string
	}{
		{"open channel", func(int) (*rabbitmqtest.Channel, error) { return nil, cause }, "open a channel"},
		{"declare topology", func(int) (*rabbitmqtest.Channel, error) {
			ch := rabbitmqtest.NewChannel()
			ch.ExchangeDeclareErr = func(int) error { return cause }
			return ch, nil
		}, "declare exchange"},
		{"enable confirms", func(int) (*rabbitmqtest.Channel, error) {
			ch := rabbitmqtest.NewChannel()
			ch.ConfirmErr = func(int) error { return cause }
			return ch, nil
		}, "publisher confirms"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn := rabbitmqtest.NewConnection()
			conn.ChannelFunc = tt.channel
			d := rabbitmqtest.NewDialer(conn)

			p, err := newProducer("u", d.Dialer(), zap.NewNop(), testDelay, testDelay, testConfirm)

			require.Nil(t, p)
			require.ErrorIs(t, err, cause)
			require.ErrorContains(t, err, tt.want)
			require.Equal(t, startupConnectAttempts, conn.CloseCalls)
			for _, ch := range conn.Channels {
				require.True(t, ch.Closed)
			}
		})
	}
}

func TestCloseLogsConnectionCloseFailure(t *testing.T) {
	logger, logs := observedLogger()
	conn := rabbitmqtest.NewConnection()
	conn.CloseErr = errors.New("close failed")
	d := rabbitmqtest.NewDialer(conn)
	p, err := newProducer("u", d.Dialer(), logger, testDelay, testDelay, testConfirm)
	require.NoError(t, err)

	p.Close()

	require.Equal(t, 1, logs.FilterMessage("error closing producer connection").Len())
}

func TestCloseIgnoresAlreadyClosedConnection(t *testing.T) {
	logger, logs := observedLogger()
	conn := rabbitmqtest.NewConnection()
	conn.CloseErr = amqp.ErrClosed
	d := rabbitmqtest.NewDialer(conn)
	p, err := newProducer("u", d.Dialer(), logger, testDelay, testDelay, testConfirm)
	require.NoError(t, err)

	p.Close()

	require.Equal(t, 1, conn.CloseCalls)
	require.Zero(t, logs.Len())
}

func TestSetupFailureLogsChannelCloseFailure(t *testing.T) {
	logger, logs := observedLogger()
	cause := errors.New("setup failed")
	conn := rabbitmqtest.NewConnection()
	conn.ChannelFunc = func(int) (*rabbitmqtest.Channel, error) {
		ch := rabbitmqtest.NewChannel()
		ch.ConfirmErr = func(int) error { return cause }
		ch.CloseErr = func(int) error { return errors.New("close failed") }
		return ch, nil
	}
	d := rabbitmqtest.NewDialer(conn)

	_, err := newProducer("u", d.Dialer(), logger, testDelay, testDelay, testConfirm)

	require.ErrorIs(t, err, cause)
	require.Equal(t, startupConnectAttempts, logs.FilterMessage("error closing producer channel").Len())
}

func TestSetupFailureIgnoresAlreadyClosedChannel(t *testing.T) {
	logger, logs := observedLogger()
	conn := rabbitmqtest.NewConnection()
	conn.ChannelFunc = func(int) (*rabbitmqtest.Channel, error) {
		ch := rabbitmqtest.NewChannel()
		ch.ConfirmErr = func(int) error { return errors.New("setup failed") }
		ch.CloseErr = func(int) error { return amqp.ErrClosed }
		return ch, nil
	}
	d := rabbitmqtest.NewDialer(conn)

	_, err := newProducer("u", d.Dialer(), logger, testDelay, testDelay, testConfirm)

	require.Error(t, err)
	require.Zero(t, logs.Len())
}
