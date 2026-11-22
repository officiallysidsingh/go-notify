// Package app wires the gRPC server and the worker processes.
package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	pb "github.com/officiallysidsingh/go-notify/api/generated"
	"github.com/officiallysidsingh/go-notify/config"
	"github.com/officiallysidsingh/go-notify/internal/auth"
	grpcserver "github.com/officiallysidsingh/go-notify/internal/grpc"
	healthmon "github.com/officiallysidsingh/go-notify/internal/health"
	"github.com/officiallysidsingh/go-notify/internal/metrics"
	"github.com/officiallysidsingh/go-notify/internal/producer"
	"github.com/officiallysidsingh/go-notify/internal/rabbitmq"
	"github.com/officiallysidsingh/go-notify/internal/ratelimiter"
	"github.com/officiallysidsingh/go-notify/internal/repository"
	"github.com/officiallysidsingh/go-notify/internal/sweeper"
)

const shutdownTimeout = 5 * time.Second

// serverDeps holds everything RunServer builds and later tears down.
type serverDeps struct {
	db        *repository.DB
	limiter   *ratelimiter.RateLimiter
	publisher *producer.RabbitMQProducer
	registry  *prometheus.Registry
	metrics   *metrics.Server
	grpc      *grpc.Server
	health    *health.Server
}

// RunServer runs the gRPC server until ctx is cancelled, then shuts down
// cleanly. It returns the first startup error, or nil after a clean shutdown.
func RunServer(ctx context.Context, cfg *config.Config, logger *zap.Logger) error {
	deps, err := buildServerDeps(cfg, logger)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", cfg.GRPC.Port)
	if err != nil {
		closeServerDeps(deps)
		return fmt.Errorf("listen on %s: %w", cfg.GRPC.Port, err)
	}
	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()
	var wg sync.WaitGroup
	httpServer, serveErr := startServerGoroutines(runCtx, &wg, cfg, logger, deps, listener)

	var runErr error
	select {
	case <-ctx.Done():
	case runErr = <-serveErr:
	}
	logger.Info("shutting down server")
	cancelRun()
	wg.Wait() // monitor and sweeper stop using deps before they are closed
	shutdownServer(deps, httpServer)
	return runErr
}

func buildServerDeps(cfg *config.Config, logger *zap.Logger) (*serverDeps, error) {
	// The key ring is parsed before any connection is opened so that a bad
	// auth.apiKeys fails fast without needing live services.
	authInterceptor, err := buildAuthInterceptor(cfg.Auth)
	if err != nil {
		return nil, err
	}
	db, err := repository.NewDB(cfg.Postgres)
	if err != nil {
		return nil, fmt.Errorf("postgres: %w", err)
	}
	limiter := ratelimiter.NewRateLimiter(cfg.Redis.Addr, cfg.Redis.Limit, cfg.Redis.Window)
	publisher, err := producer.NewProducer(cfg.RabbitMQ.URL, rabbitmq.Dial, logger)
	if err != nil {
		_ = limiter.Close()
		_ = db.Close()
		return nil, fmt.Errorf("rabbitmq: %w", err)
	}
	registry := newRegistry()
	deps := &serverDeps{
		db: db, limiter: limiter, publisher: publisher,
		registry: registry, metrics: metrics.NewServer(registry),
		health: health.NewServer(),
	}
	deps.grpc = buildGRPCServer(cfg, logger, deps, authInterceptor)
	return deps, nil
}

func buildAuthInterceptor(cfg config.AuthConfig) (grpc.UnaryServerInterceptor, error) {
	if cfg.Disabled {
		return auth.AllowAll(), nil
	}
	ring, err := auth.ParseKeys(cfg.APIKeys)
	if err != nil {
		return nil, fmt.Errorf("parse API keys: %w", err)
	}
	return auth.UnaryInterceptor(ring,
		"/grpc.health.v1.Health/Check",
		"/grpc.health.v1.Health/Watch",
	), nil
}

// newRegistry returns a registry with the Go and process collectors, shared by
// the server and the worker.
func newRegistry() *prometheus.Registry {
	registry := prometheus.NewRegistry()
	registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	return registry
}

// buildGRPCServer chains interceptors logging -> metrics -> auth, so rejected
// calls are still logged and counted.
func buildGRPCServer(cfg *config.Config, logger *zap.Logger, deps *serverDeps, authInterceptor grpc.UnaryServerInterceptor) *grpc.Server {
	server := grpc.NewServer(grpc.ChainUnaryInterceptor(
		grpcserver.LoggingInterceptor(logger),
		grpcserver.MetricsInterceptor(deps.metrics),
		authInterceptor,
	))
	pb.RegisterNotificationServiceServer(server, grpcserver.NewNotificationServer(grpcserver.Dependencies{
		Publisher:   deps.publisher,
		Store:       deps.db,
		RateLimiter: deps.limiter,
		Metrics:     deps.metrics,
		Logger:      logger,
	}))
	grpc_health_v1.RegisterHealthServer(server, deps.health)
	if cfg.GRPC.Reflection {
		reflection.Register(server)
	}
	return server
}

func startServerGoroutines(ctx context.Context, wg *sync.WaitGroup, cfg *config.Config, logger *zap.Logger, deps *serverDeps, listener net.Listener) (*http.Server, <-chan error) {
	httpServer := &http.Server{
		Addr:              cfg.Metrics.Port,
		Handler:           metricsMux(deps.registry),
		ReadHeaderTimeout: shutdownTimeout,
	}
	serveErr := make(chan error, 2)
	go func() {
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- fmt.Errorf("metrics server on %s: %w", httpServer.Addr, err)
		}
	}()
	monitor := healthmon.NewMonitor(map[string]healthmon.Check{
		"postgres": deps.db.Ping,
		"redis":    deps.limiter.Ping,
		"rabbitmq": deps.publisher.Ping,
	}, cfg.Health.Interval, logger)
	wg.Add(2)
	go func() {
		defer wg.Done()
		monitor.Run(ctx, func(healthy bool) {
			// A flip racing with shutdown must not mark the server SERVING.
			if ctx.Err() == nil {
				setServing(deps.health, healthy)
			}
		})
	}()
	go func() {
		defer wg.Done()
		sweeper.New(deps.db, deps.publisher, cfg.Sweep, deps.metrics, logger).Run(ctx)
	}()
	go func() {
		if err := deps.grpc.Serve(listener); err != nil {
			serveErr <- fmt.Errorf("gRPC serve: %w", err)
		}
	}()
	return httpServer, serveErr
}

func metricsMux(registry *prometheus.Registry) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
	return mux
}

func setServing(h *health.Server, healthy bool) {
	status := grpc_health_v1.HealthCheckResponse_NOT_SERVING
	if healthy {
		status = grpc_health_v1.HealthCheckResponse_SERVING
	}
	h.SetServingStatus("", status)
}

func shutdownServer(deps *serverDeps, httpServer *http.Server) {
	setServing(deps.health, false)
	deps.grpc.GracefulStop()
	shutdownHTTP(httpServer)
	closeServerDeps(deps)
}

func shutdownHTTP(server *http.Server) {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	_ = server.Shutdown(ctx)
}

func closeServerDeps(deps *serverDeps) {
	deps.publisher.Close()
	_ = deps.limiter.Close()
	_ = deps.db.Close()
}
