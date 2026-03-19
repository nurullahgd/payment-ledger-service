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

func (r *IdempotencyRepository) CheckOrRecord(ctx context.Context, merchantID, reference string, responsePayload string) (string, bool, error) {
	key := fmt.Sprintf("idempotency:%s:%s", merchantID, reference)

	err := r.client.SetArgs(ctx, key, responsePayload, redis.SetArgs{
		Mode: "NX",
		TTL:  r.ttl,
	}).Err()

	if err != nil {
		if errors.Is(err, redis.Nil) {
			existingPayload, getErr := r.client.Get(ctx, key).Result()
			if getErr != nil {
				return "", false, fmt.Errorf("failed to get existing idempotency key: %w", getErr)
			}
			return existingPayload, true, nil
		}
		return "", false, fmt.Errorf("failed to execute SET with NX option: %w", err)
	}

	return responsePayload, false, nil
}
