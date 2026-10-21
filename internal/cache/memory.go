package cache

import (
	"context"
	"strings"
	"sync"
	"time"
)

type memoryEntry struct {
	value   []byte
	expires time.Time
}

type Memory struct {
	mu         sync.RWMutex
	items      map[string]memoryEntry
	maxEntries int
	maxBytes   int64
	bytes      int64
	ttl        time.Duration
	allowMu    sync.Mutex
	allows     map[string][]time.Time
}

func NewMemory(maxEntries int, maxBytes int64, ttl time.Duration) *Memory {
	if maxEntries <= 0 {
		maxEntries = 2000
	}
	if maxBytes <= 0 {
		maxBytes = 64 << 20
	}
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	return &Memory{
		items:      make(map[string]memoryEntry),
		maxEntries: maxEntries,
		maxBytes:   maxBytes,
		ttl:        ttl,
		allows:     make(map[string][]time.Time),
	}
}

func (m *Memory) Driver() string {
	return "memory"
}

func (m *Memory) Get(ctx context.Context, key string) ([]byte, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	now := time.Now()

	m.mu.RLock()
	entry, ok := m.items[key]
	m.mu.RUnlock()
	if !ok {
		return nil, false, nil
	}
	if now.After(entry.expires) {
		m.mu.Lock()
		if current, exists := m.items[key]; exists && now.After(current.expires) {
			delete(m.items, key)
			m.bytes -= int64(len(key) + len(current.value))
		}
		m.mu.Unlock()
		return nil, false, nil
	}
	return entry.value, true, nil
}

func (m *Memory) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if ttl <= 0 {
		ttl = m.ttl
	}
	size := int64(len(key) + len(value))
	if size > m.maxBytes {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if old, ok := m.items[key]; ok {
		m.bytes -= int64(len(key) + len(old.value))
	}
	m.items[key] = memoryEntry{
		value:   value,
		expires: time.Now().Add(ttl),
	}
	m.bytes += size
	m.evictLocked(time.Now())
	return nil
}

func (m *Memory) evictLocked(now time.Time) {
	if len(m.items) <= m.maxEntries && m.bytes <= m.maxBytes {
		return
	}
	for key, entry := range m.items {
		if now.After(entry.expires) {
			delete(m.items, key)
			m.bytes -= int64(len(key) + len(entry.value))
		}
	}
	for len(m.items) > 0 && (len(m.items) > m.maxEntries || m.bytes > m.maxBytes) {
		for key, entry := range m.items {
			delete(m.items, key)
			m.bytes -= int64(len(key) + len(entry.value))
			break
		}
	}
}

func (m *Memory) Delete(ctx context.Context, keys ...string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, key := range keys {
		if entry, ok := m.items[key]; ok {
			delete(m.items, key)
			m.bytes -= int64(len(key) + len(entry.value))
		}
	}
	return nil
}

func (m *Memory) DeleteMatching(ctx context.Context, contains string) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if contains == "" {
		return 0, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	deleted := 0
	for key, entry := range m.items {
		if strings.Contains(key, contains) {
			delete(m.items, key)
			m.bytes -= int64(len(key) + len(entry.value))
			deleted++
		}
	}
	return deleted, nil
}

func (m *Memory) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if limit <= 0 || window <= 0 {
		return true, nil
	}
	now := time.Now()
	m.allowMu.Lock()
	defer m.allowMu.Unlock()
	history := m.allows[key]
	valid := history[:0]
	for _, at := range history {
		if now.Sub(at) < window {
			valid = append(valid, at)
		}
	}
	if len(valid) >= limit {
		m.allows[key] = valid
		return false, nil
	}
	m.allows[key] = append(valid, now)
	m.trimAllowsLocked(now, window, key)
	return true, nil
}

func (m *Memory) trimAllowsLocked(now time.Time, window time.Duration, keep string) {
	if len(m.allows) <= 10000 {
		return
	}
	for key, history := range m.allows {
		if len(history) == 0 || now.Sub(history[len(history)-1]) >= window {
			delete(m.allows, key)
		}
	}
	for len(m.allows) > 10000 {
		for key := range m.allows {
			if key == keep {
				continue
			}
			delete(m.allows, key)
			break
		}
	}
}

func (m *Memory) Close() error {
	return nil
}
