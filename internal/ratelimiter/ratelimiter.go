package ratelimiter

import (
	"context"
	"fmt"
	"time"

	"github.com/go-redis/redis/v8"
)

// Implements a simple counter-based rate limiter
type RateLimiter struct {
	client *redis.Client
	limit  int
	window time.Duration
}

// Creates a new RateLimiter
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

// Returns true if key (userID) is within rate limits
func (rl *RateLimiter) Allow(ctx context.Context, key string) (bool, error) {
	fullKey := fmt.Sprintf("rate:%s", key)
	count, err := rl.client.Incr(ctx, fullKey).Result()
	if err != nil {
		return false, err
	}
	if count == 1 {
		rl.client.Expire(ctx, fullKey, rl.window)
	}
	if count > int64(rl.limit) {
		return false, nil
	}
	return true, nil
}
