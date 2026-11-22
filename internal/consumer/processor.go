package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/streadway/amqp"

	"github.com/officiallysidsingh/go-notify/internal/notification"
)

var errNoSenderForType = errors.New("no sender configured for notification type")

// Sender delivers a notification over one channel (push, email, ...).
type Sender interface {
	Send(ctx context.Context, message notification.Message) error
}

type StatusStore interface {
	UpdateNotificationStatus(ctx context.Context, id int64, status string) error
}

type RetryPolicy struct {
	MaxAttempts int
	Delay       time.Duration
}

// Processor delivers one queued notification and settles it with the broker:
// acknowledged once sent, dead-lettered once it cannot be sent.
type Processor struct {
	store   StatusStore
	senders map[string]Sender
	retry   RetryPolicy
}

func NewProcessor(store StatusStore, senders map[string]Sender, retry RetryPolicy) *Processor {
	return &Processor{store: store, senders: senders, retry: retry}
}

func (p *Processor) Process(ctx context.Context, delivery amqp.Delivery) {
	var message notification.Message
	if err := json.Unmarshal(delivery.Body, &message); err != nil {
		log.Printf("Dead-lettering malformed message: %v", err)
		deadLetter(delivery)
		return
	}

	if err := p.send(ctx, message); err != nil {
		log.Printf("Failed to send notification %d: %v", message.NotificationID, err)
		p.recordStatus(ctx, message.NotificationID, notification.StatusFailed)
		deadLetter(delivery)
		return
	}

	// A failed status update must not requeue the message: the notification
	// has already been delivered and would be sent again.
	p.recordStatus(ctx, message.NotificationID, notification.StatusSent)
	acknowledge(delivery)
}

func (p *Processor) send(ctx context.Context, message notification.Message) error {
	sender, found := p.senders[message.Type]
	if !found {
		return fmt.Errorf("%w: %q", errNoSenderForType, message.Type)
	}

	var err error
	for attempt := 1; attempt <= p.retry.MaxAttempts; attempt++ {
		if err = sender.Send(ctx, message); err == nil {
			return nil
		}
		if attempt == p.retry.MaxAttempts || !waitOrDone(ctx, p.retry.Delay) {
			break
		}
	}
	return fmt.Errorf("gave up after %d attempts: %w", p.retry.MaxAttempts, err)
}

func (p *Processor) recordStatus(ctx context.Context, notificationID int64, status string) {
	if err := p.store.UpdateNotificationStatus(ctx, notificationID, status); err != nil {
		log.Printf("Failed to mark notification %d as %s: %v", notificationID, status, err)
	}
}

func acknowledge(delivery amqp.Delivery) {
	const multiple = false
	if err := delivery.Ack(multiple); err != nil {
		log.Printf("Error acknowledging message: %v", err)
	}
}

func deadLetter(delivery amqp.Delivery) {
	const multiple, requeue = false, false
	if err := delivery.Nack(multiple, requeue); err != nil {
		log.Printf("Error dead-lettering message: %v", err)
	}
}

func waitOrDone(ctx context.Context, delay time.Duration) bool {
	select {
	case <-time.After(delay):
		return true
	case <-ctx.Done():
		return false
	}
}
