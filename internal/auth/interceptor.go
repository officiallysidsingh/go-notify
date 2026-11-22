package auth

import (
	"context"
	"slices"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type callerKeyType struct{}

var callerKey = callerKeyType{}

// UnaryInterceptor rejects calls without a known API key and attaches the
// caller's name to the context. Methods in exemptMethods (exact, case-sensitive
// full method names) skip authentication.
func UnaryInterceptor(keys KeyRing, exemptMethods ...string) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if slices.Contains(exemptMethods, info.FullMethod) {
			return handler(ctx, req)
		}
		md, _ := metadata.FromIncomingContext(ctx)
		values := md.Get(MetadataKey)
		if len(values) == 0 {
			return nil, status.Error(codes.Unauthenticated, "missing api key")
		}
		name, ok := keys.Caller(values[0])
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "invalid api key")
		}
		ctx = setCaller(ctx, name)
		return handler(ctx, req)
	}
}

// AllowAll is used when auth is disabled; every caller is named "anonymous".
func AllowAll() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		ctx = setCaller(ctx, "anonymous")
		return handler(ctx, req)
	}
}

// callerHolder is a mutable slot installed by an outer interceptor so that the
// caller authenticated by an inner interceptor is visible outward after the
// handler returns (a derived context only flows inward).
type callerHolder struct {
	name string
}

// EnsureCallerContext returns a context carrying a caller holder, reusing the
// existing one if present. Interceptors outside auth that need the caller after
// the handler returns must call it before invoking the handler.
func EnsureCallerContext(ctx context.Context) context.Context {
	if _, ok := ctx.Value(callerKey).(*callerHolder); ok {
		return ctx
	}
	return context.WithValue(ctx, callerKey, &callerHolder{})
}

// setCaller records name in the context's holder, installing one if absent,
// and returns the context to pass inward.
func setCaller(ctx context.Context, name string) context.Context {
	ctx = EnsureCallerContext(ctx)
	ctx.Value(callerKey).(*callerHolder).name = name
	return ctx
}

// CallerFromContext returns the authenticated caller name, or "" if none.
func CallerFromContext(ctx context.Context) string {
	if h, ok := ctx.Value(callerKey).(*callerHolder); ok {
		return h.name
	}
	return ""
}
