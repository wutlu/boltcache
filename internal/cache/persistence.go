package cache

import (
	"encoding/json"
	"log"
	"os"
)

import (
	logger "boltcache/logger"
)

// Persistence
func (c *BoltCache) LoadFromDisk() {
	if c.PersistFile == "" {
		return
	}

	data, err := os.ReadFile(c.PersistFile)
	if err != nil {
		return
	}

	var items map[string]*CacheItem
	if err := json.Unmarshal(data, &items); err != nil {
		logger.Log("Failed to unmarshal persistence data: %v", err)
		return
	}

	for k, v := range items {
		c.Data.Store(k, v)
	}

	log.Printf("Loaded %d items from disk", len(items))
}
