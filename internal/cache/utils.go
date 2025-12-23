package cache

import (
	"encoding/json"
	"os"
	"time"
	"path/filepath"
)

import (
	logger "boltcache/logger"
)


func (c *BoltCache) CleanupExpiredWithConfig() {
	interval := c.Config.Cache.CleanupInterval
	if interval == 0 {
		interval = time.Minute
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now()
		expiredKeys := make([]interface{}, 0)

		c.Data.Range(func(key, value interface{}) bool {
			item := value.(*CacheItem)
			if !item.ExpiresAt.IsZero() && now.After(item.ExpiresAt) {
				expiredKeys = append(expiredKeys, key)
			}
			return true
		})

		for _, key := range expiredKeys {
			c.Data.Delete(key.(string))
		}

		if len(expiredKeys) > 0 {
			logger.Log("Cleaned up %d expired keys", len(expiredKeys))
		}
	}
}

func (c *BoltCache) PersistToDiskWithConfig() {
	if !c.Config.Persistence.Enabled {
		return
	}

	interval := c.Config.Persistence.Interval
	if interval == 0 {
		interval = 30 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		c.ForcePersist()
		c.cleanOldDataFiles(&c.Config.Persistence)
	}
}

func (c *BoltCache) ForcePersist() {
	if !c.Config.Persistence.Enabled {
		return
	}

	items := make(map[string]*CacheItem)
	c.Data.Range(func(key, value interface{}) bool {
		items[key.(string)] = value.(*CacheItem)
		return true
	})

	data, err := json.Marshal(items)
	if err != nil {
		logger.Log("Failed to marshal data: %v", err)
		return
	}

	// Create backup if enabled
	if c.Config.Persistence.BackupCount > 0 {
		c.CreateBackup()
	}

	// Write to file
	os.MkdirAll(filepath.Dir(c.Config.Persistence.File), 0755)
	if err := os.WriteFile(c.Config.Persistence.File, data, 0644); err != nil {
		logger.Log("Failed to write persistence file: %v", err)
	}
}

func (c *BoltCache) CreateBackup() {
	// Backup implementation
	backupFile := c.Config.Persistence.File + ".backup." + time.Now().Format("20060102-150405")

	if data, err := os.ReadFile(c.Config.Persistence.File); err == nil {
		os.WriteFile(backupFile, data, 0644)
	}
}



