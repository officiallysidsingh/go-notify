package grpc

import (
	"context"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/officiallysidsingh/go-notify/internal/auth"
	"github.com/officiallysidsingh/go-notify/internal/metrics"
)

// MetricsInterceptor records request count and duration per method. Chain it
// outside the auth interceptor; it installs the caller holder so the caller set
// by auth is visible here. Requests rejected by auth carry an empty caller.
func MetricsInterceptor(m *metrics.Server) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		start := time.Now()
		ctx = auth.EnsureCallerContext(ctx)
		resp, err := handler(ctx, req)
		m.Requests.WithLabelValues(
			info.FullMethod, auth.CallerFromContext(ctx), status.Code(err).String(),
		).Inc()
		m.RequestDuration.WithLabelValues(info.FullMethod).Observe(time.Since(start).Seconds())
		return resp, err
	}
}

// LoggingInterceptor logs one line per request: method, caller, code and
// duration. It never logs the request or response body.
func LoggingInterceptor(logger *zap.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		start := time.Now()
		ctx = auth.EnsureCallerContext(ctx)
		resp, err := handler(ctx, req)
		code := status.Code(err)
		log := logger.Info
		if code != codes.OK {
			log = logger.Warn
		}
		log("grpc request",
			zap.String("method", info.FullMethod),
			zap.String("caller", auth.CallerFromContext(ctx)),
			zap.String("code", code.String()),
			zap.Duration("duration", time.Since(start)),
		)
		return resp, err
	}
}
