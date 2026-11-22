package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/streadway/amqp"
	"go.uber.org/zap"

	"github.com/officiallysidsingh/go-notify/internal/metrics"
	"github.com/officiallysidsingh/go-notify/internal/notification"
	"github.com/officiallysidsingh/go-notify/internal/repository"
	"github.com/officiallysidsingh/go-notify/internal/service"
)

const statusUpdateTimeout = 5 * time.Second

var errNoSenderForType = errors.New("no sender configured for notification type")

const (
	outcomeSent    = "sent"
	outcomeFailed  = "failed"
	outcomeSkipped = "skipped"
)

// Sender delivers a notification over one channel (push, email, ...).
type Sender interface {
	Send(ctx context.Context, message notification.Message) error
}

type StatusStore interface {
	GetNotificationStatus(ctx context.Context, id int64) (string, error)
	UpdateNotificationStatus(ctx context.Context, id int64, status string) error
}

type RetryPolicy struct {
	MaxAttempts int
	Delay       time.Duration
}

// Processor delivers one queued notification and settles it with the broker:
// acknowledged once sent (or already settled), dead-lettered once it cannot be sent.
type Processor struct {
	store   StatusStore
	senders map[string]Sender
	retry   RetryPolicy
	metrics *metrics.Worker
	logger  *zap.Logger
}

func NewProcessor(store StatusStore, senders map[string]Sender, retry RetryPolicy, m *metrics.Worker, logger *zap.Logger) *Processor {
	return &Processor{store: store, senders: senders, retry: retry, metrics: m, logger: logger}
}

func (p *Processor) Process(ctx context.Context, delivery amqp.Delivery) {
	var message notification.Message
	if err := json.Unmarshal(delivery.Body, &message); err != nil {
		p.logger.Warn("dead-lettering malformed message", zap.Error(err))
		p.deadLetter(p.logger, delivery)
		return
	}
	logger := p.logger.With(zap.Int64("notification_id", message.NotificationID))

	if p.settleWithoutSending(ctx, logger, delivery, message) {
		return
	}
	p.deliver(ctx, logger, delivery, message)
}

// settleWithoutSending handles notifications that must not be sent: ones that
// already reached a final status and ones whose row is gone. It reports
// whether it settled the delivery.
func (p *Processor) settleWithoutSending(ctx context.Context, logger *zap.Logger, delivery amqp.Delivery, message notification.Message) bool {
	status, err := p.store.GetNotificationStatus(ctx, message.NotificationID)
	switch {
	case errors.Is(err, repository.ErrNotificationNotFound):
		logger.Warn("dead-lettering notification missing from the store")
		p.deadLetter(logger, delivery)
		return true
	case err != nil:
		logger.Warn("could not look up notification status; sending anyway", zap.Error(err))
		return false
	case notification.IsSettled(status):
		logger.Info("skipping notification that is already settled", zap.String("status", status))
		p.metrics.Sends.WithLabelValues(message.Type, outcomeSkipped).Inc()
		p.acknowledge(logger, delivery)
		return true
	}
	return false
}

func (p *Processor) deliver(ctx context.Context, logger *zap.Logger, delivery amqp.Delivery, message notification.Message) {
	sender, found := p.senders[message.Type]
	if !found {
		err := fmt.Errorf("%w: %q", errNoSenderForType, message.Type)
		p.fail(ctx, logger, delivery, message, err)
		return
	}

	err := p.sendWithRetries(ctx, logger, sender, message)
	switch {
	case err == nil:
		// A failed status update must not requeue the message: the
		// notification has already been delivered and would be sent again.
		p.recordStatus(ctx, logger, message.NotificationID, notification.StatusSent)
		p.metrics.Sends.WithLabelValues(message.Type, outcomeSent).Inc()
		p.acknowledge(logger, delivery)
	case errors.Is(context.Cause(ctx), ErrSessionAborted):
		// The connection is gone: leave the delivery unsettled so the broker
		// redelivers it. A per-job timeout is not this case; the channel is
		// still open, so it fails and dead-letters below.
		logger.Info("stopped before settling notification", zap.Error(err))
	default:
		p.fail(ctx, logger, delivery, message, err)
	}
}

func (p *Processor) fail(ctx context.Context, logger *zap.Logger, delivery amqp.Delivery, message notification.Message, cause error) {
	logger.Warn("failed to send notification", zap.Error(cause))
	p.recordStatus(ctx, logger, message.NotificationID, notification.StatusFailed)
	p.metrics.Sends.WithLabelValues(message.Type, outcomeFailed).Inc()
	p.deadLetter(logger, delivery)
}

// sendWithRetries tries up to MaxAttempts times, waiting between attempts for
// the sender's retry hint or else the configured delay. It stops early, with
// the last error, when ctx ends.
func (p *Processor) sendWithRetries(ctx context.Context, logger *zap.Logger, sender Sender, message notification.Message) error {
	var err error
	for attempt := 1; attempt <= p.retry.MaxAttempts; attempt++ {
		if err = p.timedSend(ctx, sender, message); err == nil {
			return nil
		}
		logger.Warn("send attempt failed", zap.Int("attempt", attempt), zap.Error(err))
		if attempt == p.retry.MaxAttempts || !waitOrDone(ctx, p.retryDelay(err)) {
			break
		}
	}
	return fmt.Errorf("gave up after %d attempts: %w", p.retry.MaxAttempts, err)
}

func (p *Processor) timedSend(ctx context.Context, sender Sender, message notification.Message) error {
	start := time.Now()
	err := sender.Send(ctx, message)
	p.metrics.SendDuration.WithLabelValues(message.Type).Observe(time.Since(start).Seconds())
	return err
}

func (p *Processor) retryDelay(sendErr error) time.Duration {
	var hint *service.RetryAfterError
	if errors.As(sendErr, &hint) && hint.Delay > 0 {
		return hint.Delay
	}
	return p.retry.Delay
}

func (p *Processor) recordStatus(ctx context.Context, logger *zap.Logger, notificationID int64, status string) {
	// The job context may have just timed out; the status must still be written.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), statusUpdateTimeout)
	defer cancel()
	if err := p.store.UpdateNotificationStatus(ctx, notificationID, status); err != nil {
		logger.Error("failed to record notification status", zap.String("status", status), zap.Error(err))
	}
}

func (p *Processor) acknowledge(logger *zap.Logger, delivery amqp.Delivery) {
	const multiple = false
	if err := delivery.Ack(multiple); err != nil {
		logger.Error("failed to acknowledge message", zap.Error(err))
	}
}

func (p *Processor) deadLetter(logger *zap.Logger, delivery amqp.Delivery) {
	const multiple, requeue = false, false
	p.metrics.DeadLettered.Inc()
	if err := delivery.Nack(multiple, requeue); err != nil {
		logger.Error("failed to dead-letter message", zap.Error(err))
	}
}

func waitOrDone(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return ctx.Err() == nil
	case <-ctx.Done():
		return false
	}
}
