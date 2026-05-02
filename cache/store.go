package cache

import "time"

// Store is the unified cache interface used by the engine.
type Store interface {
	Get(key string) (interface{}, bool)
	Set(key string, value interface{}, ttl time.Duration)
	Delete(key string)
}
