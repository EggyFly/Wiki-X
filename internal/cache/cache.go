package cache

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Cache interface {
	Get(ctx context.Context, key string) ([]byte, bool, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	Delete(ctx context.Context, keys ...string) error
	DeleteMatching(ctx context.Context, contains string) (int, error)
	Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, error)
	Close() error
	Driver() string
}

type Options struct {
	Driver        string
	TTL           time.Duration
	MaxEntries    int
	MaxBytes      int64
	RedisAddr     string
	RedisPassword string
	RedisDB       int
	RedisPrefix   string
}

func New(opts Options) (Cache, error) {
	driver := strings.ToLower(strings.TrimSpace(opts.Driver))
	if driver == "" {
		driver = "auto"
	}
	if opts.TTL <= 0 {
		opts.TTL = 10 * time.Minute
	}

	switch driver {
	case "memory":
		return NewMemory(opts.MaxEntries, opts.MaxBytes, opts.TTL), nil
	case "redis":
		if strings.TrimSpace(opts.RedisAddr) == "" {
			return nil, errors.New("redis address is empty")
		}
		return newRedis(opts)
	case "auto":
		if strings.TrimSpace(opts.RedisAddr) == "" {
			return NewMemory(opts.MaxEntries, opts.MaxBytes, opts.TTL), nil
		}
		redisCache, err := newRedis(opts)
		if err != nil {
			return NewMemory(opts.MaxEntries, opts.MaxBytes, opts.TTL), nil
		}
		return redisCache, nil
	default:
		return nil, fmt.Errorf("unknown cache driver %q", opts.Driver)
	}
}
