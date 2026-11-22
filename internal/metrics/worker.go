package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Worker holds the metrics recorded by the notification worker.
type Worker struct {
	Sends        *prometheus.CounterVec
	SendDuration *prometheus.HistogramVec
	DeadLettered prometheus.Counter
	Reconnects   prometheus.Counter
}

// NewWorker creates the worker metrics and registers them on registerer.
// Sends carries an outcome label of "sent", "failed" or "skipped".
func NewWorker(registerer prometheus.Registerer) *Worker {
	factory := promauto.With(registerer)
	return &Worker{
		Sends: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "gonotify_sends_total",
			Help: "Send attempts by notification type and outcome.",
		}, []string{"type", "outcome"}),
		SendDuration: factory.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "gonotify_send_duration_seconds",
			Help:    "Send duration by notification type.",
			Buckets: prometheus.DefBuckets,
		}, []string{"type"}),
		DeadLettered: factory.NewCounter(prometheus.CounterOpts{
			Name: "gonotify_dead_lettered_total",
			Help: "Messages routed to the dead-letter queue.",
		}),
		Reconnects: factory.NewCounter(prometheus.CounterOpts{
			Name: "gonotify_broker_reconnects_total",
			Help: "Broker reconnections.",
		}),
	}
}
