package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func familyNames(t *testing.T, reg *prometheus.Registry) map[string]float64 {
	t.Helper()
	families, err := reg.Gather()
	require.NoError(t, err)
	names := make(map[string]float64, len(families))
	for _, f := range families {
		var total float64
		for _, m := range f.GetMetric() {
			if m.GetCounter() != nil {
				total += m.GetCounter().GetValue()
			}
		}
		names[f.GetName()] = total
	}
	return names
}

func TestNewServerRegistersOnGivenRegistry(t *testing.T) {
	reg := prometheus.NewRegistry()
	s := NewServer(reg)

	s.Received.Inc()

	assert.Equal(t, 1.0, testutil.ToFloat64(s.Received))
	assert.Equal(t, 1.0, familyNames(t, reg)["notifications_received_total"])
}

func TestNewServerTwiceOnSeparateRegistriesDoesNotPanic(t *testing.T) {
	assert.NotPanics(t, func() {
		_ = NewServer(prometheus.NewRegistry())
		_ = NewServer(prometheus.NewRegistry())
	})
}

func TestNewServerRegistersEveryMetric(t *testing.T) {
	reg := prometheus.NewRegistry()
	s := NewServer(reg)

	s.Requests.WithLabelValues("/m", "c", "OK").Inc()
	s.RequestDuration.WithLabelValues("/m").Observe(0.1)
	s.RateLimited.Inc()
	s.SweepRepublished.Inc()
	s.SweepFailures.Inc()

	names := familyNames(t, reg)
	for _, name := range []string{
		"gonotify_grpc_requests_total",
		"gonotify_grpc_request_duration_seconds",
		"gonotify_rate_limited_total",
		"gonotify_sweep_republished_total",
		"gonotify_sweep_failures_total",
	} {
		assert.Contains(t, names, name)
	}
}
