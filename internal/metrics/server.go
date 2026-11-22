// Package metrics owns every Prometheus counter and histogram used by the service.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Server holds the metrics recorded by the gRPC server and the sweeper.
type Server struct {
	Received         prometheus.Counter
	Requests         *prometheus.CounterVec
	RequestDuration  *prometheus.HistogramVec
	RateLimited      prometheus.Counter
	SweepRepublished prometheus.Counter
	SweepFailures    prometheus.Counter
}

// NewServer creates the server metrics and registers them on registerer.
func NewServer(registerer prometheus.Registerer) *Server {
	factory := promauto.With(registerer)
	return &Server{
		Received: factory.NewCounter(prometheus.CounterOpts{
			Name: "notifications_received_total",
			Help: "Notifications accepted over gRPC.",
		}),
		Requests: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "gonotify_grpc_requests_total",
			Help: "gRPC requests by method, caller and status code.",
		}, []string{"method", "caller", "code"}),
		RequestDuration: factory.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "gonotify_grpc_request_duration_seconds",
			Help:    "gRPC request duration by method.",
			Buckets: prometheus.DefBuckets,
		}, []string{"method"}),
		RateLimited: factory.NewCounter(prometheus.CounterOpts{
			Name: "gonotify_rate_limited_total",
			Help: "Requests rejected by the rate limiter.",
		}),
		SweepRepublished: factory.NewCounter(prometheus.CounterOpts{
			Name: "gonotify_sweep_republished_total",
			Help: "Stale notifications republished by the sweeper.",
		}),
		SweepFailures: factory.NewCounter(prometheus.CounterOpts{
			Name: "gonotify_sweep_failures_total",
			Help: "Sweeper republish attempts that failed.",
		}),
	}
}
