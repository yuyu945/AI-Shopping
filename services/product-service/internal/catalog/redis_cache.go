package catalog

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

type redisDetailCache struct{ client *redis.Client }

func NewRedisDetailCache(client *redis.Client) DetailCache { return &redisDetailCache{client: client} }

func (c *redisDetailCache) Get(ctx context.Context, key string) ([]byte, error) {
	value, err := c.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, ErrCacheMiss
	}
	return value, err
}

func (c *redisDetailCache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	return c.client.Set(ctx, key, value, ttl).Err()
}

func (c *redisDetailCache) Delete(ctx context.Context, key string) error {
	return c.client.Del(ctx, key).Err()
}
