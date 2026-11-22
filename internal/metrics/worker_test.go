package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
)

func TestNewWorkerRegistersEveryMetric(t *testing.T) {
	reg := prometheus.NewRegistry()
	w := NewWorker(reg)

	w.Sends.WithLabelValues("push", "sent").Inc()
	w.SendDuration.WithLabelValues("push").Observe(0.2)
	w.DeadLettered.Inc()
	w.Reconnects.Inc()

	names := familyNames(t, reg)
	for _, name := range []string{
		"gonotify_sends_total",
		"gonotify_send_duration_seconds",
		"gonotify_dead_lettered_total",
		"gonotify_broker_reconnects_total",
	} {
		assert.Contains(t, names, name)
	}
	assert.Equal(t, 1.0, testutil.ToFloat64(w.Sends.WithLabelValues("push", "sent")))
}

func TestNewWorkerAndServerShareOneRegistry(t *testing.T) {
	reg := prometheus.NewRegistry()
	assert.NotPanics(t, func() {
		NewServer(reg)
		NewWorker(reg)
	})
}
