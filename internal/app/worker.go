package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"

	"github.com/officiallysidsingh/go-notify/config"
	"github.com/officiallysidsingh/go-notify/internal/consumer"
	healthmon "github.com/officiallysidsingh/go-notify/internal/health"
	"github.com/officiallysidsingh/go-notify/internal/metrics"
	"github.com/officiallysidsingh/go-notify/internal/notification"
	"github.com/officiallysidsingh/go-notify/internal/rabbitmq"
	"github.com/officiallysidsingh/go-notify/internal/repository"
	"github.com/officiallysidsingh/go-notify/internal/service"
)

const reconnectDelay = 5 * time.Second

// RunWorker consumes notifications until ctx is cancelled, then drains the
// consumer and shuts down. It returns a startup error, or nil after a clean
// shutdown.
func RunWorker(ctx context.Context, cfg *config.Config, logger *zap.Logger) error {
	db, err := repository.NewDB(cfg.Postgres)
	if err != nil {
		return fmt.Errorf("postgres: %w", err)
	}
	registry := newRegistry()
	workerMetrics := metrics.NewWorker(registry)
	c := buildConsumer(cfg, db, workerMetrics, logger)
	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()
	var wg sync.WaitGroup
	httpServer, serveErr := startWorkerHTTP(runCtx, &wg, cfg, logger, db, c, registry)

	// A failed HTTP listener cancels the run so the consumer stops too.
	var runErr error
	done := make(chan struct{})
	go func() {
		select {
		case runErr = <-serveErr:
			cancelRun()
		case <-done:
		}
	}()
	c.Run(runCtx)
	close(done)

	logger.Info("shutting down worker")
	cancelRun()
	wg.Wait()
	shutdownHTTP(httpServer)
	_ = db.Close()
	if runErr == nil {
		select {
		case runErr = <-serveErr:
		default:
		}
	}
	return runErr
}

func buildConsumer(cfg *config.Config, db *repository.DB, m *metrics.Worker, logger *zap.Logger) *consumer.Consumer {
	// Email and SMS have no sender yet; the processor fails and dead-letters
	// those notifications instead of reporting them as sent.
	senders := map[string]consumer.Sender{
		notification.TypePush: service.NewNtfySender(cfg.Ntfy),
	}
	retry := consumer.RetryPolicy{MaxAttempts: cfg.Worker.SendAttempts, Delay: cfg.Worker.RetryDelay}
	processor := consumer.NewProcessor(db, senders, retry, m, logger)
	return consumer.NewConsumer(consumer.Options{
		URL:            cfg.RabbitMQ.URL,
		Workers:        cfg.Worker.Concurrency,
		ReconnectDelay: reconnectDelay,
		Dial:           rabbitmq.Dial,
	}, processor, m, logger)
}

func startWorkerHTTP(ctx context.Context, wg *sync.WaitGroup, cfg *config.Config, logger *zap.Logger, db *repository.DB, c *consumer.Consumer, registry *prometheus.Registry) (*http.Server, <-chan error) {
	monitor := healthmon.NewMonitor(map[string]healthmon.Check{
		"postgres": db.Ping,
		"consumer": func(context.Context) error {
			if !c.Connected() {
				return errors.New("not connected")
			}
			return nil
		},
	}, cfg.Health.Interval, logger)
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
	mux.Handle("/healthz", monitor.Handler())
	server := &http.Server{Addr: cfg.Worker.MetricsPort, Handler: mux, ReadHeaderTimeout: shutdownTimeout}
	serveErr := make(chan error, 1)
	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- fmt.Errorf("worker HTTP server on %s: %w", server.Addr, err)
		}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		monitor.Run(ctx, func(bool) {})
	}()
	return server, serveErr
}
