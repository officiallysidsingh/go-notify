// Package rabbitmqtest provides scriptable fakes for rabbitmq.Connection and rabbitmq.Channel.
package rabbitmqtest

import (
	"sync"

	"github.com/streadway/amqp"

	"github.com/officiallysidsingh/go-notify/internal/rabbitmq"
)

// ErrFunc scripts an error for the Nth (1-based) call of a method; nil means success.
type ErrFunc func(call int) error

type ExchangeDecl struct {
	Name, Kind string
}

type QueueDecl struct {
	Name string
	Args amqp.Table
}

type BindingDecl struct {
	Queue, Key, Exchange string
}

// Channel is a fake rabbitmq.Channel. Set the knobs before use; read the
// recorded fields only after the code under test has stopped using it.
type Channel struct {
	mu sync.Mutex

	Published            []amqp.Publishing
	PublishedRoutingKeys []string
	DeclaredExchanges    []ExchangeDecl
	DeclaredQueues       []QueueDecl
	Bindings             []BindingDecl
	QosPrefetchCount     int
	Consumers            map[string]<-chan amqp.Delivery
	Cancelled            []string
	ConfirmEnabled       bool
	PublishConfirmChan   chan amqp.Confirmation
	Closed               bool

	// ConsumeDeliveries supplies the channel returned by Consume for a queue.
	ConsumeDeliveries map[string]<-chan amqp.Delivery

	ExchangeDeclareErr ErrFunc
	QueueDeclareErr    ErrFunc
	QueueBindErr       ErrFunc
	QosErr             ErrFunc
	ConfirmErr         ErrFunc
	PublishErr         ErrFunc
	ConsumeErr         ErrFunc
	CancelErr          ErrFunc
	CloseErr           ErrFunc

	// AutoConfirm, when set, answers each successful Publish with the given
	// ack value on the confirm channel.
	AutoConfirm *bool

	calls map[string]int
}

var _ rabbitmq.Channel = (*Channel)(nil)

func NewChannel() *Channel {
	return &Channel{
		Consumers:         map[string]<-chan amqp.Delivery{},
		ConsumeDeliveries: map[string]<-chan amqp.Delivery{},
		calls:             map[string]int{},
	}
}

// Calls returns how many times the named method was called.
func (c *Channel) Calls(method string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls[method]
}

// TotalOps returns the number of declare/bind calls made.
func (c *Channel) TotalOps() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.DeclaredExchanges) + len(c.DeclaredQueues) + len(c.Bindings)
}

func (c *Channel) next(method string, f ErrFunc) error {
	c.calls[method]++
	if f == nil {
		return nil
	}
	return f(c.calls[method])
}

func (c *Channel) ExchangeDeclare(name, kind string, durable, autoDelete, internal, noWait bool, args amqp.Table) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.next("ExchangeDeclare", c.ExchangeDeclareErr); err != nil {
		return err
	}
	c.DeclaredExchanges = append(c.DeclaredExchanges, ExchangeDecl{Name: name, Kind: kind})
	return nil
}

func (c *Channel) QueueDeclare(name string, durable, autoDelete, exclusive, noWait bool, args amqp.Table) (amqp.Queue, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.next("QueueDeclare", c.QueueDeclareErr); err != nil {
		return amqp.Queue{}, err
	}
	c.DeclaredQueues = append(c.DeclaredQueues, QueueDecl{Name: name, Args: args})
	return amqp.Queue{Name: name}, nil
}

func (c *Channel) QueueBind(name, key, exchange string, noWait bool, args amqp.Table) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.next("QueueBind", c.QueueBindErr); err != nil {
		return err
	}
	c.Bindings = append(c.Bindings, BindingDecl{Queue: name, Key: key, Exchange: exchange})
	return nil
}

func (c *Channel) Qos(prefetchCount, prefetchSize int, global bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.next("Qos", c.QosErr); err != nil {
		return err
	}
	c.QosPrefetchCount = prefetchCount
	return nil
}

func (c *Channel) Confirm(noWait bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.next("Confirm", c.ConfirmErr); err != nil {
		return err
	}
	c.ConfirmEnabled = true
	return nil
}

func (c *Channel) NotifyPublish(confirm chan amqp.Confirmation) chan amqp.Confirmation {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.PublishConfirmChan = confirm
	return confirm
}

func (c *Channel) Publish(exchange, key string, mandatory, immediate bool, msg amqp.Publishing) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.next("Publish", c.PublishErr); err != nil {
		return err
	}
	c.Published = append(c.Published, msg)
	c.PublishedRoutingKeys = append(c.PublishedRoutingKeys, key)
	if c.AutoConfirm != nil && c.PublishConfirmChan != nil {
		c.PublishConfirmChan <- amqp.Confirmation{Ack: *c.AutoConfirm}
	}
	return nil
}

// ConfirmNext delivers a broker ack (true) or nack (false).
func (c *Channel) ConfirmNext(ack bool) {
	c.mu.Lock()
	ch := c.PublishConfirmChan
	c.mu.Unlock()
	ch <- amqp.Confirmation{Ack: ack}
}

func (c *Channel) Consume(queue, consumer string, autoAck, exclusive, noLocal, noWait bool, args amqp.Table) (<-chan amqp.Delivery, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.next("Consume", c.ConsumeErr); err != nil {
		return nil, err
	}
	d, ok := c.ConsumeDeliveries[queue]
	if !ok {
		d = make(chan amqp.Delivery)
	}
	c.Consumers[consumer] = d
	return d, nil
}

func (c *Channel) Cancel(consumer string, noWait bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.next("Cancel", c.CancelErr); err != nil {
		return err
	}
	c.Cancelled = append(c.Cancelled, consumer)
	return nil
}

func (c *Channel) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Closed = true
	return c.next("Close", c.CloseErr)
}

// Connection is a fake rabbitmq.Connection.
type Connection struct {
	mu sync.Mutex

	Channels []*Channel
	// ChannelFunc overrides channel creation; call is 1-based.
	ChannelFunc   func(call int) (*Channel, error)
	CloseErr      error
	IsClosedValue bool
	CloseCalls    int

	closeNotify []chan *amqp.Error
	channelCall int
}

var _ rabbitmq.Connection = (*Connection)(nil)

func NewConnection() *Connection { return &Connection{} }

func (c *Connection) Channel() (rabbitmq.Channel, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.channelCall++
	var ch *Channel
	if c.ChannelFunc != nil {
		var err error
		ch, err = c.ChannelFunc(c.channelCall)
		if err != nil {
			return nil, err
		}
	} else {
		ch = NewChannel()
	}
	c.Channels = append(c.Channels, ch)
	return ch, nil
}

func (c *Connection) NotifyClose(receiver chan *amqp.Error) chan *amqp.Error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closeNotify = append(c.closeNotify, receiver)
	return receiver
}

// FireClose simulates the broker closing the connection.
func (c *Connection) FireClose(err *amqp.Error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.IsClosedValue = true
	for _, r := range c.closeNotify {
		r <- err
	}
}

func (c *Connection) IsClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.IsClosedValue
}

func (c *Connection) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.CloseCalls++
	return c.CloseErr
}

// Dialer is a rabbitmq.Dialer that hands out scripted outcomes in order.
type Dialer struct {
	mu    sync.Mutex
	steps []DialResult
	// Dialed records the connections returned (nil entries for failures).
	Dialed []*Connection
	Calls  int
}

// DialResult is one dial outcome.
type DialResult struct {
	Conn *Connection
	Err  error
}

// NewDialer cycles through conns, returning each in turn (the last repeats).
func NewDialer(conns ...*Connection) *Dialer {
	steps := make([]DialResult, len(conns))
	for i, c := range conns {
		steps[i] = DialResult{Conn: c}
	}
	return &Dialer{steps: steps}
}

// NewScriptedDialer returns the given results in order (the last repeats).
func NewScriptedDialer(steps ...DialResult) *Dialer {
	return &Dialer{steps: steps}
}

// Dial implements rabbitmq.Dialer.
func (d *Dialer) Dial(url string) (rabbitmq.Connection, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	i := d.Calls
	d.Calls++
	if len(d.steps) == 0 {
		d.Dialed = append(d.Dialed, nil)
		return nil, amqp.ErrClosed
	}
	if i >= len(d.steps) {
		i = len(d.steps) - 1
	}
	s := d.steps[i]
	d.Dialed = append(d.Dialed, s.Conn)
	if s.Err != nil {
		return nil, s.Err
	}
	return s.Conn, nil
}

// Dialer returns d.Dial as a rabbitmq.Dialer.
func (d *Dialer) Dialer() rabbitmq.Dialer { return d.Dial }

// DialCount is the number of dial calls so far.
func (d *Dialer) DialCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.Calls
}
