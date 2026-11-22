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
		return handler(context.WithValue(ctx, callerKey, name), req)
	}
}

// AllowAll is used when auth is disabled; every caller is named "anonymous".
func AllowAll() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		return handler(context.WithValue(ctx, callerKey, "anonymous"), req)
	}
}

// CallerFromContext returns the authenticated caller name, or "" if none.
func CallerFromContext(ctx context.Context) string {
	name, _ := ctx.Value(callerKey).(string)
	return name
}
