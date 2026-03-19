package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type IdempotencyRepository struct {
	client *redis.Client
	ttl    time.Duration
}

func NewIdempotencyRepository(client *redis.Client, ttl time.Duration) *IdempotencyRepository {
	return &IdempotencyRepository{
		client: client,
		ttl:    ttl,
	}
}

func (r *IdempotencyRepository) Get(ctx context.Context, merchantID, reference string) (string, bool, error) {
	key := fmt.Sprintf("idempotency:%s:%s", merchantID, reference)

	val, err := r.client.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("failed to get idempotency record: %w", err)
	}

	return val, true, nil
}

func (r *IdempotencyRepository) Set(ctx context.Context, merchantID, reference, payload string) error {
	key := fmt.Sprintf("idempotency:%s:%s", merchantID, reference)

	if err := r.client.Set(ctx, key, payload, r.ttl).Err(); err != nil {
		return fmt.Errorf("failed to set idempotency record: %w", err)
	}

	return nil
}
