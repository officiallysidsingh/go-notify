package ratelimiter

import (
	"context"
	"fmt"
	"time"

	"github.com/go-redis/redis/v8"
)

// Incrementing and setting the expiry must happen atomically: a counter left
// without an expiry would block its user forever.
var incrementWithExpiry = redis.NewScript(`
local count = redis.call('INCR', KEYS[1])
if redis.call('PTTL', KEYS[1]) < 0 then
	redis.call('PEXPIRE', KEYS[1], ARGV[1])
end
return count
`)

// RateLimiter is a fixed-window counter backed by Redis.
type RateLimiter struct {
	client *redis.Client
	limit  int
	window time.Duration
}

func NewRateLimiter(addr string, limit int, window time.Duration) *RateLimiter {
	client := redis.NewClient(&redis.Options{
		Addr: addr,
	})

	return &RateLimiter{
		client: client,
		limit:  limit,
		window: window,
	}
}

// Allow reports whether key is still within its rate limit for the current window.
func (rl *RateLimiter) Allow(ctx context.Context, key string) (bool, error) {
	counterKey := fmt.Sprintf("rate:%s", key)
	count, err := incrementWithExpiry.Run(
		ctx, rl.client, []string{counterKey}, rl.window.Milliseconds(),
	).Int64()
	if err != nil {
		return false, fmt.Errorf("failed to count request for %q: %w", key, err)
	}
	return count <= int64(rl.limit), nil
}

func (rl *RateLimiter) Close() error {
	return rl.client.Close()
}

// Ping verifies the Redis connection is reachable.
func (rl *RateLimiter) Ping(ctx context.Context) error {
	if err := rl.client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("rate limiter unreachable: %w", err)
	}
	return nil
}
