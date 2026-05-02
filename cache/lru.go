package cache

import (
	"sync"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
)

type lruEntry struct {
	value   interface{}
	expires time.Time
}

// LRUStore is an in-process LRU cache with TTL support.
type LRUStore struct {
	mu   sync.Mutex
	impl *lru.Cache[string, lruEntry]
}

// NewLRUStore creates an LRU cache with the given capacity.
func NewLRUStore(capacity int) (*LRUStore, error) {
	impl, err := lru.New[string, lruEntry](capacity)
	if err != nil {
		return nil, err
	}
	return &LRUStore{impl: impl}, nil
}

func (s *LRUStore) Get(key string) (interface{}, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.impl.Get(key)
	if !ok {
		return nil, false
	}
	if !entry.expires.IsZero() && time.Now().After(entry.expires) {
		s.impl.Remove(key)
		return nil, false
	}
	return entry.value, true
}

func (s *LRUStore) Set(key string, value interface{}, ttl time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var exp time.Time
	if ttl > 0 {
		exp = time.Now().Add(ttl)
	}
	s.impl.Add(key, lruEntry{value: value, expires: exp})
}

func (s *LRUStore) Delete(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.impl.Remove(key)
}
