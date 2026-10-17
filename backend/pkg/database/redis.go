package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const redisOpTimeout = 2 * time.Second

// ErrNotFound is returned by Redis.Get when the key does not exist.
var ErrNotFound = errors.New("not found")

// Redis wraps the client with per-operation timeouts. The embedded Client is
// exposed for scripts and pub/sub.
type Redis struct {
	*redis.Client
}

func NewRedis(ctx context.Context, url string) (*Redis, error) {
	opt, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("parse redis url: %w", err)
	}
	r := &Redis{redis.NewClient(opt)}
	if err := r.PingTimeout(ctx); err != nil {
		_ = r.Close()
		return nil, err
	}
	return r, nil
}

func (r *Redis) PingTimeout(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, redisOpTimeout)
	defer cancel()
	if err := r.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("ping redis: %w", err)
	}
	return nil
}

func (r *Redis) GetString(ctx context.Context, key string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, redisOpTimeout)
	defer cancel()
	v, err := r.Client.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return "", ErrNotFound
	}
	return v, err
}

func (r *Redis) SetString(ctx context.Context, key, val string, ttl time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, redisOpTimeout)
	defer cancel()
	return r.Client.Set(ctx, key, val, ttl).Err()
}

func (r *Redis) Delete(ctx context.Context, keys ...string) error {
	ctx, cancel := context.WithTimeout(ctx, redisOpTimeout)
	defer cancel()
	return r.Client.Del(ctx, keys...).Err()
}

func (r *Redis) Increment(ctx context.Context, key string) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, redisOpTimeout)
	defer cancel()
	return r.Client.Incr(ctx, key).Result()
}
