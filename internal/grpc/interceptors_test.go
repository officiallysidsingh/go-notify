package grpc

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	pb "github.com/officiallysidsingh/go-notify/api/generated"
	"github.com/officiallysidsingh/go-notify/internal/auth"
	"github.com/officiallysidsingh/go-notify/internal/metrics"
)

const testMethod = "/svc/Method"

// runInterceptor invokes the interceptor for testMethod with the given request
// and handler, returning the handler's outcome.
func runInterceptor(
	ctx context.Context,
	interceptor grpc.UnaryServerInterceptor,
	req any,
	handler grpc.UnaryHandler,
) error {
	_, err := interceptor(ctx, req, &grpc.UnaryServerInfo{FullMethod: testMethod}, handler)
	return err
}

func returning(err error) grpc.UnaryHandler {
	return func(context.Context, any) (any, error) { return nil, err }
}

// chain runs outer then inner, mirroring grpc.ChainUnaryInterceptor.
func chain(outer, inner grpc.UnaryServerInterceptor, handler grpc.UnaryHandler) grpc.UnaryHandler {
	info := &grpc.UnaryServerInfo{FullMethod: testMethod}
	return func(ctx context.Context, req any) (any, error) {
		return outer(ctx, req, info, func(ctx context.Context, req any) (any, error) {
			return inner(ctx, req, info, handler)
		})
	}
}

func TestMetricsInterceptorCountsRequestByCallerAndCode(t *testing.T) {
	m := metrics.NewServer(prometheus.NewRegistry())
	handler := chain(MetricsInterceptor(m), auth.AllowAll(), returning(status.Error(codes.NotFound, "x")))

	_, err := handler(context.Background(), nil)

	require.Equal(t, codes.NotFound, status.Code(err))
	assert.Equal(t, 1.0, testutil.ToFloat64(m.Requests.WithLabelValues(testMethod, "anonymous", "NotFound")))
}

func TestMetricsInterceptorUsesCallerFromContext(t *testing.T) {
	m := metrics.NewServer(prometheus.NewRegistry())
	keys, err := auth.ParseKeys("billing:secret")
	require.NoError(t, err)
	handler := chain(MetricsInterceptor(m), auth.UnaryInterceptor(keys), returning(nil))
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(auth.MetadataKey, "secret"))

	_, err = handler(ctx, nil)

	require.NoError(t, err)
	assert.Equal(t, 1.0, testutil.ToFloat64(m.Requests.WithLabelValues(testMethod, "billing", "OK")))
}

func TestMetricsInterceptorLabelsUnauthenticatedCallerEmpty(t *testing.T) {
	m := metrics.NewServer(prometheus.NewRegistry())

	err := runInterceptor(context.Background(), MetricsInterceptor(m), nil,
		returning(status.Error(codes.Unauthenticated, "no key")))

	require.Error(t, err)
	assert.Equal(t, 1.0, testutil.ToFloat64(m.Requests.WithLabelValues(testMethod, "", "Unauthenticated")))
}

func TestMetricsInterceptorObservesDuration(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := metrics.NewServer(reg)

	require.NoError(t, runInterceptor(context.Background(), MetricsInterceptor(m), nil, returning(nil)))

	assert.Equal(t, 1, testutil.CollectAndCount(m.RequestDuration))
	families, err := reg.Gather()
	require.NoError(t, err)
	var found bool
	for _, f := range families {
		if f.GetName() == "gonotify_grpc_request_duration_seconds" {
			found = true
			assert.Equal(t, uint64(1), f.GetMetric()[0].GetHistogram().GetSampleCount())
		}
	}
	assert.True(t, found, "duration histogram not registered")
}

func newObservedLogger() (*zap.Logger, *observer.ObservedLogs) {
	core, logs := observer.New(zapcore.InfoLevel)
	return zap.New(core), logs
}

func TestLoggingInterceptorLogsMethodCallerAndCode(t *testing.T) {
	logger, logs := newObservedLogger()
	handler := chain(LoggingInterceptor(logger), auth.AllowAll(), returning(nil))

	_, err := handler(context.Background(), nil)

	require.NoError(t, err)
	require.Equal(t, 1, logs.Len())
	fields := logs.All()[0].ContextMap()
	assert.Equal(t, testMethod, fields["method"])
	assert.Equal(t, "anonymous", fields["caller"])
	assert.Equal(t, "OK", fields["code"])
	assert.IsType(t, time.Duration(0), fields["duration"])
}

func TestLoggingInterceptorDoesNotLogMessageBody(t *testing.T) {
	logger, logs := newObservedLogger()
	req := &pb.NotificationRequest{Message: "top-secret"}

	err := runInterceptor(context.Background(), LoggingInterceptor(logger), req,
		returning(status.Error(codes.InvalidArgument, "bad")))

	require.Error(t, err)
	require.NotZero(t, logs.Len())
	for _, entry := range logs.All() {
		assert.NotContains(t, entry.Message, "top-secret")
		for _, v := range entry.ContextMap() {
			assert.NotContains(t, fmt.Sprint(v), "top-secret")
		}
	}
}

func TestLoggingInterceptorLogsFailuresAtWarn(t *testing.T) {
	logger, logs := newObservedLogger()
	interceptor := LoggingInterceptor(logger)

	_ = runInterceptor(context.Background(), interceptor, nil, returning(status.Error(codes.InvalidArgument, "bad")))
	_ = runInterceptor(context.Background(), interceptor, nil, returning(nil))

	require.Equal(t, 2, logs.Len())
	assert.Equal(t, zapcore.WarnLevel, logs.All()[0].Level)
	assert.Equal(t, zapcore.InfoLevel, logs.All()[1].Level)
}
