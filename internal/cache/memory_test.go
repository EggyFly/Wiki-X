package cache

import (
	"context"
	"testing"
	"time"
)

func TestMemoryGetSetDelete(t *testing.T) {
	c := NewMemory(10, 1<<20, time.Minute)
	ctx := context.Background()

	if err := c.Set(ctx, "key", []byte("value"), time.Minute); err != nil {
		t.Fatal(err)
	}
	value, ok, err := c.Get(ctx, "key")
	if err != nil || !ok || string(value) != "value" {
		t.Fatalf("get = %q, %v, %v", value, ok, err)
	}
	if err := c.Delete(ctx, "key"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := c.Get(ctx, "key"); ok {
		t.Fatal("key should be deleted")
	}
}

func TestMemoryDeleteMatching(t *testing.T) {
	c := NewMemory(10, 1<<20, time.Minute)
	ctx := context.Background()
	for key, value := range map[string]string{
		":/guide/:one": "1",
		":/guide/:two": "2",
		":/other/:one": "3",
	} {
		if err := c.Set(ctx, key, []byte(value), time.Minute); err != nil {
			t.Fatal(err)
		}
	}
	deleted, err := c.DeleteMatching(ctx, ":/guide/:")
	if err != nil || deleted != 2 {
		t.Fatalf("deleted = %d, %v", deleted, err)
	}
	if _, ok, _ := c.Get(ctx, ":/other/:one"); !ok {
		t.Fatal("unrelated key should remain")
	}
}

func TestMemoryAllow(t *testing.T) {
	c := NewMemory(10, 1<<20, time.Minute)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		allowed, err := c.Allow(ctx, "comment:1", 3, 50*time.Millisecond)
		if err != nil || !allowed {
			t.Fatalf("allow %d = %v, %v", i, allowed, err)
		}
	}
	if allowed, err := c.Allow(ctx, "comment:1", 3, 50*time.Millisecond); err != nil || allowed {
		t.Fatalf("limit not enforced: %v, %v", allowed, err)
	}
	if allowed, err := c.Allow(ctx, "comment:2", 3, 50*time.Millisecond); err != nil || !allowed {
		t.Fatalf("other key blocked: %v, %v", allowed, err)
	}
	time.Sleep(60 * time.Millisecond)
	if allowed, err := c.Allow(ctx, "comment:1", 3, 50*time.Millisecond); err != nil || !allowed {
		t.Fatalf("window did not reset: %v, %v", allowed, err)
	}
	if allowed, err := c.Allow(ctx, "disabled", 0, time.Minute); err != nil || !allowed {
		t.Fatalf("disabled limit = %v, %v", allowed, err)
	}
}

func TestMemoryExpires(t *testing.T) {
	c := NewMemory(10, 1<<20, 20*time.Millisecond)
	ctx := context.Background()
	if err := c.Set(ctx, "key", []byte("value"), 20*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Millisecond)
	if _, ok, _ := c.Get(ctx, "key"); ok {
		t.Fatal("key should expire")
	}
}

func TestMemoryMaxEntries(t *testing.T) {
	c := NewMemory(1, 1<<20, time.Minute)
	ctx := context.Background()
	_ = c.Set(ctx, "a", []byte("1"), time.Minute)
	_ = c.Set(ctx, "b", []byte("2"), time.Minute)

	count := 0
	for _, key := range []string{"a", "b"} {
		if _, ok, _ := c.Get(ctx, key); ok {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("cached entries = %d, want 1", count)
	}
}

func TestMemoryMaxBytes(t *testing.T) {
	c := NewMemory(10, 5, time.Minute)
	ctx := context.Background()
	if err := c.Set(ctx, "key", []byte("value"), time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := c.Get(ctx, "key"); ok {
		t.Fatal("oversized entry should not be cached")
	}
}
