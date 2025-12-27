package cache

import (
	"sync"
)

const ShardCount = 2048

type Shard struct {
	mu    sync.RWMutex
	items map[string]interface{}
}

type ShardedMap struct {
	shards []*Shard
}

func NewShardedMap() *ShardedMap {
	sm := &ShardedMap{
		shards: make([]*Shard, ShardCount),
	}
	for i := 0; i < ShardCount; i++ {
		sm.shards[i] = &Shard{
			items: make(map[string]interface{}, 1024),
		}
	}
	return sm
}

func (sm *ShardedMap) getShard(key string) *Shard {
	var h uint32 = 2166136261
	for i := 0; i < len(key); i++ {
		h ^= uint32(key[i])
		h *= 16777619
	}
	return sm.shards[h&(ShardCount-1)]
}

func (sm *ShardedMap) Load(key string) (interface{}, bool) {
	shard := sm.getShard(key)
	shard.mu.RLock()
	val, ok := shard.items[key]
	shard.mu.RUnlock()
	return val, ok
}

func (sm *ShardedMap) Store(key string, value interface{}) {
	shard := sm.getShard(key)
	shard.mu.Lock()
	shard.items[key] = value
	shard.mu.Unlock()
}

func (sm *ShardedMap) Delete(key string) {
	shard := sm.getShard(key)
	shard.mu.Lock()
	delete(shard.items, key)
	shard.mu.Unlock()
}

func (sm *ShardedMap) Range(f func(key, value interface{}) bool) {
	for _, shard := range sm.shards {
		shard.mu.RLock()
		for k, v := range shard.items {
			if !f(k, v) {
				shard.mu.RUnlock()
				return
			}
		}
		shard.mu.RUnlock()
	}
}

func (sm *ShardedMap) Len() int {
	total := 0
	for _, shard := range sm.shards {
		shard.mu.RLock()
		total += len(shard.items)
		shard.mu.RUnlock()
	}
	return total
}
