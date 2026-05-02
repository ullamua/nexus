package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisStore uses Redis as the cache backend.
type RedisStore struct {
	client *redis.Client
}

// NewRedisStore creates a RedisStore connected to the given URL.
func NewRedisStore(redisURL string) (*RedisStore, error) {
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("cache: parse redis URL: %w", err)
	}
	client := redis.NewClient(opts)
	if err := client.Ping(context.Background()).Err(); err != nil {
		return nil, fmt.Errorf("cache: redis ping: %w", err)
	}
	return &RedisStore{client: client}, nil
}

func (s *RedisStore) Get(key string) (interface{}, bool) {
	raw, err := s.client.Get(context.Background(), key).Bytes()
	if err != nil {
		return nil, false
	}
	var v interface{}
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, false
	}
	return v, true
}

func (s *RedisStore) Set(key string, value interface{}, ttl time.Duration) {
	b, err := json.Marshal(value)
	if err != nil {
		return
	}
	s.client.Set(context.Background(), key, b, ttl)
}

func (s *RedisStore) Delete(key string) {
	s.client.Del(context.Background(), key)
}
