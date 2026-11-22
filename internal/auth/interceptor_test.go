package auth

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const healthCheck = "/grpc.health.v1.Health/Check"

func newRing(t *testing.T) KeyRing {
	t.Helper()
	ring, err := ParseKeys("billing:k1,orders:k2")
	require.NoError(t, err)
	return ring
}

func call(t *testing.T, ic grpc.UnaryServerInterceptor, ctx context.Context, method string) (called bool, caller string, err error) {
	t.Helper()
	handler := func(ctx context.Context, req any) (any, error) {
		called = true
		caller = CallerFromContext(ctx)
		return "ok", nil
	}
	_, err = ic(ctx, nil, &grpc.UnaryServerInfo{FullMethod: method}, handler)
	return called, caller, err
}

func withKey(key string) context.Context {
	return metadata.NewIncomingContext(context.Background(), metadata.Pairs(MetadataKey, key))
}

func TestInterceptorRejectsMissingKey(t *testing.T) {
	called, _, err := call(t, UnaryInterceptor(newRing(t)), context.Background(), "/svc/Method")
	assert.False(t, called)
	assert.Equal(t, codes.Unauthenticated, status.Code(err))
}

func TestInterceptorRejectsUnknownKey(t *testing.T) {
	called, _, err := call(t, UnaryInterceptor(newRing(t)), withKey("wrong"), "/svc/Method")
	assert.False(t, called)
	assert.Equal(t, codes.Unauthenticated, status.Code(err))
}

func TestInterceptorPassesCallerToHandler(t *testing.T) {
	called, caller, err := call(t, UnaryInterceptor(newRing(t)), withKey("k1"), "/svc/Method")
	require.NoError(t, err)
	assert.True(t, called)
	assert.Equal(t, "billing", caller)
}

func TestInterceptorSkipsExemptMethod(t *testing.T) {
	ic := UnaryInterceptor(newRing(t), healthCheck)
	called, caller, err := call(t, ic, context.Background(), healthCheck)
	require.NoError(t, err)
	assert.True(t, called)
	assert.Empty(t, caller)
}

func TestAllowAllNamesCallerAnonymous(t *testing.T) {
	called, caller, err := call(t, AllowAll(), context.Background(), "/svc/Method")
	require.NoError(t, err)
	assert.True(t, called)
	assert.Equal(t, "anonymous", caller)
}
