package cache 

import (
	"time"
)

import (
	logger "boltcache/logger"
)

func (c *BoltCache) cleanupExpired() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now()
		expiredKeys := make([]string, 0)

		c.Data.Range(func(key, value interface{}) bool {
			item := value.(*CacheItem)
			if !item.ExpiresAt.IsZero() && now.After(item.ExpiresAt) {
				expiredKeys = append(expiredKeys, key.(string))
			}
			return true
		})

		for _, key := range expiredKeys {
			c.Data.Delete(key)
		}

		if len(expiredKeys) > 0 {
			logger.Log("Cleaned up %d expired keys", len(expiredKeys))
		}
	}
}