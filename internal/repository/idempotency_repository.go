package repository

import (
	"context"
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

func (r *IdempotencyRepository) CheckOrRecord(ctx context.Context, merchantID, reference string, responsePayload string) (cachedResponse string, isReplayed bool, err error) {
	key := fmt.Sprintf("idempotency:%s:%s", merchantID, reference)

	success, err := r.client.SetNX(ctx, key, responsePayload, r.ttl).Result()
	if err != nil {
		return "", false, fmt.Errorf("failed to execute SETNX on redis: %w", err)
	}

	if success {
		return responsePayload, false, nil
	}

	existingPayload, err := r.client.Get(ctx, key).Result()
	if err != nil {
		return "", false, fmt.Errorf("failed to get existing idempotency key: %w", err)
	}

	return existingPayload, true, nil
}
