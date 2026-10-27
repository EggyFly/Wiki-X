package server

import (
	"sync"
	"time"

	"github.com/EggyFly/Wiki-X/internal/store"
)

type mediaMetaEntry struct {
	item    *store.Media
	expires time.Time
}

type mediaMetaCache struct {
	mu    sync.RWMutex
	items map[string]mediaMetaEntry
	max   int
	ttl   time.Duration
}

func newMediaMetaCache(max int, ttl time.Duration) *mediaMetaCache {
	if max <= 0 {
		max = 10000
	}
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	return &mediaMetaCache{items: make(map[string]mediaMetaEntry), max: max, ttl: ttl}
}

func (c *mediaMetaCache) Get(name string) (*store.Media, bool) {
	c.mu.RLock()
	entry, ok := c.items[name]
	c.mu.RUnlock()
	if !ok || time.Now().After(entry.expires) {
		if ok {
			c.Delete(name)
		}
		return nil, false
	}
	return entry.item, true
}

func (c *mediaMetaCache) Put(name string, item *store.Media) {
	if item == nil {
		return
	}
	c.mu.Lock()
	if len(c.items) >= c.max {
		for key := range c.items {
			delete(c.items, key)
			if len(c.items) < c.max {
				break
			}
		}
	}
	c.items[name] = mediaMetaEntry{item: item, expires: time.Now().Add(c.ttl)}
	c.mu.Unlock()
}

func (c *mediaMetaCache) Delete(name string) {
	c.mu.Lock()
	delete(c.items, name)
	c.mu.Unlock()
}
