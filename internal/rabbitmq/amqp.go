// Package rabbitmq exposes the AMQP wire types through small interfaces so
// code that uses a broker is testable without one.
package rabbitmq

import "github.com/streadway/amqp"

// Channel is the subset of *amqp.Channel the application uses.
type Channel interface {
	ExchangeDeclare(name, kind string, durable, autoDelete, internal, noWait bool, args amqp.Table) error
	QueueDeclare(name string, durable, autoDelete, exclusive, noWait bool, args amqp.Table) (amqp.Queue, error)
	QueueBind(name, key, exchange string, noWait bool, args amqp.Table) error
	Qos(prefetchCount, prefetchSize int, global bool) error
	Confirm(noWait bool) error
	NotifyPublish(confirm chan amqp.Confirmation) chan amqp.Confirmation
	Publish(exchange, key string, mandatory, immediate bool, msg amqp.Publishing) error
	Consume(queue, consumer string, autoAck, exclusive, noLocal, noWait bool, args amqp.Table) (<-chan amqp.Delivery, error)
	Cancel(consumer string, noWait bool) error
	Close() error
}

// Connection is the subset of *amqp.Connection the application uses.
type Connection interface {
	Channel() (Channel, error)
	NotifyClose(receiver chan *amqp.Error) chan *amqp.Error
	IsClosed() bool
	Close() error
}

// Dialer opens a Connection to the broker at url.
type Dialer func(url string) (Connection, error)

var _ Channel = (*amqp.Channel)(nil)

type amqpConnection struct {
	*amqp.Connection
}

func (c *amqpConnection) Channel() (Channel, error) {
	ch, err := c.Connection.Channel()
	if err != nil {
		return nil, err
	}
	return ch, nil
}

// Dial is the real Dialer.
func Dial(url string) (Connection, error) {
	conn, err := amqp.Dial(url)
	if err != nil {
		return nil, err
	}
	return &amqpConnection{conn}, nil
}
