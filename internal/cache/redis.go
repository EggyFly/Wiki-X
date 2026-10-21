package cache

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	redisFailureLimit = 3
	redisOpenTime     = 10 * time.Second
)

var (
	errRedisOpen = errors.New("redis cache is temporarily unavailable")
	limitScript  = redis.NewScript(`
local now = tonumber(ARGV[1])
local window = tonumber(ARGV[2])
local limit = tonumber(ARGV[3])
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', now - window)
if redis.call('ZCARD', KEYS[1]) >= limit then
  redis.call('PEXPIRE', KEYS[1], window)
  return 0
end
redis.call('ZADD', KEYS[1], now, ARGV[4])
redis.call('PEXPIRE', KEYS[1], window)
return 1
`)
)

type Redis struct {
	client    *redis.Client
	prefix    string
	ttl       time.Duration
	mu        sync.Mutex
	failures  int
	openUntil time.Time
}

func newRedis(opts Options) (*Redis, error) {
	prefix := strings.TrimSpace(opts.RedisPrefix)
	if prefix == "" {
		prefix = "wikix"
	}
	client := redis.NewClient(&redis.Options{
		Addr:         opts.RedisAddr,
		Password:     opts.RedisPassword,
		DB:           opts.RedisDB,
		DialTimeout:  3 * time.Second,
		ReadTimeout:  time.Second,
		WriteTimeout: time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("redis %s: %w", opts.RedisAddr, err)
	}

	return &Redis{client: client, prefix: prefix, ttl: opts.TTL}, nil
}

func (c *Redis) Driver() string {
	return "redis"
}

func (c *Redis) key(key string) string {
	return c.prefix + ":" + key
}

func (c *Redis) Get(ctx context.Context, key string) ([]byte, bool, error) {
	if err := c.available(); err != nil {
		return nil, false, err
	}
	value, err := c.client.Get(ctx, c.key(key)).Bytes()
	if errors.Is(err, redis.Nil) {
		c.succeeded()
		return nil, false, nil
	}
	if err != nil {
		return nil, false, c.failed(err)
	}
	c.succeeded()
	return value, true, nil
}

func (c *Redis) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if err := c.available(); err != nil {
		return err
	}
	if ttl <= 0 {
		ttl = c.ttl
	}
	if err := c.client.Set(ctx, c.key(key), value, ttl).Err(); err != nil {
		return c.failed(err)
	}
	c.succeeded()
	return nil
}

func (c *Redis) Delete(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	if err := c.available(); err != nil {
		return err
	}
	prefixed := make([]string, len(keys))
	for i, key := range keys {
		prefixed[i] = c.key(key)
	}
	if err := c.client.Del(ctx, prefixed...).Err(); err != nil {
		return c.failed(err)
	}
	c.succeeded()
	return nil
}

func (c *Redis) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, error) {
	if limit <= 0 || window <= 0 {
		return true, nil
	}
	if err := c.available(); err != nil {
		return false, err
	}
	now := time.Now().UnixMilli()
	member := fmt.Sprintf("%d-%d", now, rand.Uint64())
	result, err := limitScript.Run(ctx, c.client, []string{c.key("limit:" + key)},
		now, window.Milliseconds(), limit, member).Int()
	if err != nil {
		return false, c.failed(err)
	}
	c.succeeded()
	return result == 1, nil
}

func (c *Redis) available() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Now().Before(c.openUntil) {
		return errRedisOpen
	}
	return nil
}

func (c *Redis) failed(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.failures++
	if c.failures >= redisFailureLimit {
		c.failures = 0
		c.openUntil = time.Now().Add(redisOpenTime)
	}
	return err
}

func (c *Redis) succeeded() {
	c.mu.Lock()
	c.failures = 0
	c.openUntil = time.Time{}
	c.mu.Unlock()
}

func (c *Redis) Close() error {
	return c.client.Close()
}
