package ratelimit

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

type Result struct {
	Allowed    bool
	RetryAfter int
}

type Limiter interface {
	Allow(ctx context.Context, key string) (Result, error)
}

type SlidingWindowLimiter struct {
	client     *redis.Client
	limit      int
	windowSecs int
}

func NewSlidingWindowLimiter(client *redis.Client, limit, windowSecs int) *SlidingWindowLimiter {
	return &SlidingWindowLimiter{
		client:     client,
		limit:      limit,
		windowSecs: windowSecs,
	}
}

func (l *SlidingWindowLimiter) Allow(ctx context.Context, key string) (Result, error) {
	now := time.Now()
	windowStart := now.Add(-time.Duration(l.windowSecs) * time.Second)
	member := strconv.FormatInt(now.UnixNano(), 10)

	pipe := l.client.Pipeline()
	pipe.ZRemRangeByScore(ctx, key, "0", strconv.FormatInt(windowStart.UnixNano(), 10))
	pipe.ZAdd(ctx, key, redis.Z{Score: float64(now.UnixNano()), Member: member})
	countCmd := pipe.ZCard(ctx, key)
	pipe.Expire(ctx, key, time.Duration(l.windowSecs)*time.Second)

	if _, err := pipe.Exec(ctx); err != nil {
		return Result{}, fmt.Errorf("rate limiter pipeline failed: %w", err)
	}

	count := countCmd.Val()
	if count > int64(l.limit) {
		return Result{Allowed: false, RetryAfter: l.windowSecs}, nil
	}

	return Result{Allowed: true}, nil
}

func MerchantKey(merchantID string) string {
	return fmt.Sprintf("ratelimit:%s", merchantID)
}
