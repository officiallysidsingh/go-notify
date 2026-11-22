package ratelimiter

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testLimit  = 2
	testWindow = time.Minute
)

func newTestLimiter(t *testing.T) (*RateLimiter, *miniredis.Miniredis) {
	t.Helper()
	server := miniredis.RunT(t)
	return NewRateLimiter(server.Addr(), testLimit, testWindow), server
}

func TestAllowPermitsRequestsUpToTheLimit(t *testing.T) {
	limiter, _ := newTestLimiter(t)

	for range testLimit {
		allowed, err := limiter.Allow(context.Background(), "user-1")
		require.NoError(t, err)
		assert.True(t, allowed)
	}
}

func TestAllowRejectsRequestsOverTheLimit(t *testing.T) {
	limiter, _ := newTestLimiter(t)
	for range testLimit {
		_, err := limiter.Allow(context.Background(), "user-1")
		require.NoError(t, err)
	}

	allowed, err := limiter.Allow(context.Background(), "user-1")

	require.NoError(t, err)
	assert.False(t, allowed)
}

func TestAllowPermitsRequestsAgainAfterTheWindow(t *testing.T) {
	limiter, server := newTestLimiter(t)
	for range testLimit + 1 {
		_, err := limiter.Allow(context.Background(), "user-1")
		require.NoError(t, err)
	}

	server.FastForward(testWindow + time.Second)
	allowed, err := limiter.Allow(context.Background(), "user-1")

	require.NoError(t, err)
	assert.True(t, allowed)
}

func TestAllowRestoresExpiryOnCounterLeftWithoutOne(t *testing.T) {
	limiter, server := newTestLimiter(t)
	require.NoError(t, server.Set("rate:user-1", "5"))

	_, err := limiter.Allow(context.Background(), "user-1")

	require.NoError(t, err)
	assert.Equal(t, testWindow, server.TTL("rate:user-1"))
}
