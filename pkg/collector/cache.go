package collector

import (
	"sync"
	"time"
)

const defaultCacheTTL = 30 * time.Minute

// CostCache provides a thread-safe in-memory cache for AWS cost data.
// Cached entries expire after 30 minutes.
type CostCache struct {
	mu        sync.RWMutex
	data      *AWSCostData
	storedAt  time.Time
	ttl       time.Duration
}

// NewCostCache creates a new CostCache with the default 30-minute TTL.
func NewCostCache() *CostCache {
	return &CostCache{
		ttl: defaultCacheTTL,
	}
}

// Get returns the cached cost data if it exists and has not expired.
// The second return value indicates whether valid cached data was found.
func (c *CostCache) Get() (*AWSCostData, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.data == nil {
		return nil, false
	}

	if time.Since(c.storedAt) > c.ttl {
		return nil, false
	}

	return c.data, true
}

// Set stores cost data in the cache with the current timestamp.
func (c *CostCache) Set(data *AWSCostData) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.data = data
	c.storedAt = time.Now()
}
