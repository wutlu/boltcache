package cache

import (
	"sync"
	"time"
)

import (
	config "boltcache/config"
)

type CacheItem struct {
	Value     interface{}
	ExpiresAt time.Time
}

type BoltCache struct {
	Data        *ShardedMap
	Subscribers sync.Map
	Mu          sync.RWMutex
	PersistFile string
	Replicas    []string
	LuaEngine *LuaEngine
	Config    *config.Config
}

var _API = (*BoltCache)(nil)

// NewBoltCache creates a new BoltCache instance
func NewBoltCache(persistFile ...string) *BoltCache {
	bc := &BoltCache{
		Data: NewShardedMap(),
	}
	if len(persistFile) > 0 {
		bc.PersistFile = persistFile[0]
	}

	go bc.cleanupExpired()
	return bc
}

func (c *BoltCache) Set(key string, value interface{}, ttl time.Duration) {
	var expiresAt time.Time
	if ttl > 0 {
		expiresAt = time.Now().Add(ttl)
	}

	item := &CacheItem{
		Value:     value,
		ExpiresAt: expiresAt,
	}

	c.Data.Store(key, item)
}

func (c *BoltCache) Get(key string) (interface{}, bool) {
	val, ok := c.Data.Load(key)
	if !ok {
		return nil, false
	}

	item := val.(*CacheItem)
	if !item.ExpiresAt.IsZero() && time.Now().After(item.ExpiresAt) {
		c.Data.Delete(key)
		return nil, false
	}

	return item.Value, true
}

func (c *BoltCache) Delete(key string) {
	c.Data.Delete(key)
}

func (c *BoltCache) AddReplica(addr string) {
	c.Mu.Lock()
	defer c.Mu.Unlock()
	c.Replicas = append(c.Replicas, addr)
}

func (c *BoltCache) GetReplicas() []string {
	c.Mu.RLock()
	defer c.Mu.RUnlock()
	return append([]string(nil), c.Replicas...) 
}

func (c *BoltCache) GetData() *ShardedMap {
	return c.Data
}
