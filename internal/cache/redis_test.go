package cache

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestRedisCache(t *testing.T) {
	addr := os.Getenv("WIKIX_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("set WIKIX_TEST_REDIS_ADDR to run redis integration test")
	}
	prefix := fmt.Sprintf("wikix-test-%d", time.Now().UnixNano())
	c, err := New(Options{
		Driver:        "redis",
		TTL:           time.Minute,
		RedisAddr:     addr,
		RedisPassword: os.Getenv("WIKIX_TEST_REDIS_PASSWORD"),
		RedisDB:       0,
		RedisPrefix:   prefix,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	ctx := context.Background()
	if err := c.Set(ctx, "key", []byte("value"), time.Minute); err != nil {
		t.Fatal(err)
	}
	value, ok, err := c.Get(ctx, "key")
	if err != nil || !ok || string(value) != "value" {
		t.Fatalf("get = %q, %v, %v", value, ok, err)
	}
	if c.Driver() != "redis" {
		t.Fatalf("driver = %q", c.Driver())
	}
	if err := c.Delete(ctx, "key"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := c.Get(ctx, "key"); ok {
		t.Fatal("key should be deleted")
	}

	if err := c.Set(ctx, ":/guide/:one", []byte("1"), time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := c.Set(ctx, ":/guide/:two", []byte("2"), time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := c.Set(ctx, ":/other/:one", []byte("3"), time.Minute); err != nil {
		t.Fatal(err)
	}
	deleted, err := c.DeleteMatching(ctx, ":/guide/:")
	if err != nil || deleted != 2 {
		t.Fatalf("delete matching = %d, %v", deleted, err)
	}
	if _, ok, _ := c.Get(ctx, ":/other/:one"); !ok {
		t.Fatal("unrelated key should remain")
	}

	limitKey := fmt.Sprintf("comment-%d", time.Now().UnixNano())
	for i := 0; i < 3; i++ {
		allowed, err := c.Allow(ctx, limitKey, 3, time.Minute)
		if err != nil || !allowed {
			t.Fatalf("allow %d = %v, %v", i, allowed, err)
		}
	}
	if allowed, err := c.Allow(ctx, limitKey, 3, time.Minute); err != nil || allowed {
		t.Fatalf("limit not enforced: %v, %v", allowed, err)
	}
	if err := c.Delete(ctx, "limit:"+limitKey); err != nil {
		t.Fatal(err)
	}
}

func TestRedisCircuitBreaker(t *testing.T) {
	client := redis.NewClient(&redis.Options{
		Addr:         "127.0.0.1:1",
		DialTimeout:  100 * time.Millisecond,
		ReadTimeout:  100 * time.Millisecond,
		WriteTimeout: 100 * time.Millisecond,
	})
	defer client.Close()

	c := &Redis{client: client, prefix: "test", ttl: time.Minute}
	ctx := context.Background()
	for i := 0; i < redisFailureLimit; i++ {
		if _, _, err := c.Get(ctx, "key"); err == nil {
			t.Fatal("expected redis error")
		}
	}
	start := time.Now()
	if _, _, err := c.Get(ctx, "key"); err == nil {
		t.Fatal("expected open circuit error")
	}
	if time.Since(start) > 50*time.Millisecond {
		t.Fatalf("circuit breaker did not short-circuit: %v", time.Since(start))
	}
}
