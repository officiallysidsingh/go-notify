// Package rabbitmq holds the exchange and queue layout shared by the producer
// and the consumer, so both always declare it identically.
package rabbitmq

import (
	"fmt"

	"github.com/streadway/amqp"

	"github.com/officiallysidsingh/go-notify/internal/notification"
)

const (
	NotificationExchange = "notification_exchange_topic"
	BroadcastExchange    = "notification_exchange_fanout"
	DeadLetterExchange   = "dead_letter_exchange"
	DeadLetterQueue      = "dead_letter_queue"

	deadLetterRoutingKey = "dead_letter"
	matchAllRoutingKey   = ""
)

func QueueForType(notificationType string) string {
	return "queue_" + notificationType
}

func DeclareTopology(ch Channel) error {
	if err := declareDeadLettering(ch); err != nil {
		return err
	}
	if err := declareDurableExchange(ch, NotificationExchange, amqp.ExchangeTopic); err != nil {
		return err
	}
	if err := declareDurableExchange(ch, BroadcastExchange, amqp.ExchangeFanout); err != nil {
		return err
	}
	for _, notificationType := range notification.Types() {
		if err := declareNotificationQueue(ch, notificationType); err != nil {
			return err
		}
	}
	return nil
}

func declareDeadLettering(ch Channel) error {
	if err := declareDurableExchange(ch, DeadLetterExchange, amqp.ExchangeDirect); err != nil {
		return err
	}
	if err := declareDurableQueue(ch, DeadLetterQueue, nil); err != nil {
		return err
	}
	return bindQueue(ch, DeadLetterQueue, deadLetterRoutingKey, DeadLetterExchange)
}

func declareNotificationQueue(ch Channel, notificationType string) error {
	queue := QueueForType(notificationType)
	deadLetterToExchange := amqp.Table{
		"x-dead-letter-exchange":    DeadLetterExchange,
		"x-dead-letter-routing-key": deadLetterRoutingKey,
	}
	if err := declareDurableQueue(ch, queue, deadLetterToExchange); err != nil {
		return err
	}
	if err := bindQueue(ch, queue, notificationType, NotificationExchange); err != nil {
		return err
	}
	return bindQueue(ch, queue, matchAllRoutingKey, BroadcastExchange)
}

func declareDurableExchange(ch Channel, name, kind string) error {
	const durable, autoDelete, internal, noWait = true, false, false, false
	if err := ch.ExchangeDeclare(name, kind, durable, autoDelete, internal, noWait, nil); err != nil {
		return fmt.Errorf("failed to declare exchange %s: %w", name, err)
	}
	return nil
}

func declareDurableQueue(ch Channel, name string, args amqp.Table) error {
	const durable, autoDelete, exclusive, noWait = true, false, false, false
	if _, err := ch.QueueDeclare(name, durable, autoDelete, exclusive, noWait, args); err != nil {
		return fmt.Errorf("failed to declare queue %s: %w", name, err)
	}
	return nil
}

func bindQueue(ch Channel, queue, routingKey, exchange string) error {
	const noWait = false
	if err := ch.QueueBind(queue, routingKey, exchange, noWait, nil); err != nil {
		return fmt.Errorf("failed to bind queue %s to %s: %w", queue, exchange, err)
	}
	return nil
}
