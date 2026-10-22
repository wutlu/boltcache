package cache

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"time"
)

// Policy selects which keys are evicted when a limit is exceeded.
type Policy string

const (
	PolicyLRU        Policy = "lru"        // least recently used
	PolicyLFU        Policy = "lfu"        // least frequently used
	PolicyRandom     Policy = "random"     // any key
	PolicyTTL        Policy = "ttl"        // nearest expiration first, then LRU
	PolicyNoEviction Policy = "noeviction" // reject writes with an OOM error
)

// ParsePolicy validates an eviction policy name.
func ParsePolicy(s string) (Policy, error) {
	switch p := Policy(strings.ToLower(s)); p {
	case PolicyLRU, PolicyLFU, PolicyRandom, PolicyTTL, PolicyNoEviction:
		return p, nil
	case "":
		return PolicyLRU, nil
	}
	return "", fmt.Errorf("invalid eviction policy %q (want lru, lfu, random, ttl or noeviction)", s)
}

// Options configures a cache.
type Options struct {
	// MaxKeys limits the number of keys; 0 means unlimited.
	MaxKeys int64
	// MaxMemory limits the approximate memory used by keys and values, in
	// bytes; 0 means unlimited.
	MaxMemory int64
	// EvictionThreshold is the fraction (0,1] of MaxMemory at which eviction
	// starts. Defaults to 1.
	EvictionThreshold float64
	// EvictionPolicy decides what happens when a limit is exceeded.
	EvictionPolicy Policy
	// CleanupInterval is how often expired keys are actively reclaimed.
	// Expired keys are never returned regardless. 0 disables the loop.
	CleanupInterval time.Duration
}

func (o *Options) normalize() {
	if o.EvictionThreshold <= 0 || o.EvictionThreshold > 1 {
		o.EvictionThreshold = 1
	}
	if o.EvictionPolicy == "" {
		o.EvictionPolicy = PolicyLRU
	}
}

func (o *Options) limited() bool { return o.MaxKeys > 0 || o.MaxMemory > 0 }

// OOM reports whether writes that grow memory must be refused: the
// noeviction policy is active and a limit is exceeded.
func (c *BoltCache) OOM() bool {
	return c.opts.EvictionPolicy == PolicyNoEviction && c.opts.limited() && c.overLimit()
}

func (c *BoltCache) overLimit() bool {
	o := &c.opts
	if o.MaxKeys > 0 && c.gkeys.Load() > o.MaxKeys {
		return true
	}
	if o.MaxMemory > 0 && float64(c.gmem.Load()) > float64(o.MaxMemory)*o.EvictionThreshold {
		return true
	}
	return false
}

// maybeEvict evicts keys until the cache is within its limits. Like Redis it
// uses sampling: a handful of random keys are inspected and the best
// candidate for the policy is removed.
func (c *BoltCache) maybeEvict() {
	if !c.opts.limited() || c.opts.EvictionPolicy == PolicyNoEviction {
		return
	}
	for tries := 0; tries < 256 && c.overLimit(); tries++ {
		if !c.evictOne() {
			return
		}
	}
}

const (
	evictionCandidates   = 16  // keys compared per eviction
	evictionKeysPerShard = 4   // keys taken from each visited shard
	evictionMaxShards    = 256 // non-empty shards visited at most
)

func (c *BoltCache) evictOne() bool {
	var (
		bestKey   string
		bestShard = -1
		bestScore int64
	)
	policy := c.opts.EvictionPolicy
	want := evictionCandidates
	if policy == PolicyRandom {
		want = 1
	}
	// Walk shards from a random starting point, skipping empty ones via
	// their key counters, until enough candidates have been sampled.
	start := rand.IntN(shardCount)
	sampled, visited := 0, 0
	for n := 0; n < shardCount && sampled < want && visited < evictionMaxShards; n++ {
		i := (start + n) & (shardCount - 1)
		s := &c.shards[i]
		if s.keys.Load() == 0 {
			continue
		}
		visited++
		s.mu.RLock()
		taken := 0
		// Go map iteration starts at a random position, which gives us
		// cheap random sampling within the shard.
		for k, e := range s.m {
			score := evictionScore(policy, e)
			if bestShard == -1 || score < bestScore {
				bestKey, bestShard, bestScore = k, i, score
			}
			sampled++
			taken++
			if taken >= evictionKeysPerShard {
				break
			}
		}
		s.mu.RUnlock()
	}
	if bestShard == -1 {
		return false
	}
	s := &c.shards[bestShard]
	s.mu.Lock()
	if e, ok := s.m[bestKey]; ok {
		c.removeLocked(s, bestKey, e)
		c.evicted.Add(1)
	}
	s.mu.Unlock()
	return true
}

// evictionScore returns a value where lower means "evict first".
func evictionScore(p Policy, e *entry) int64 {
	switch p {
	case PolicyLFU:
		return int64(e.freq.Load())<<40 | (e.access.Load()>>20)&(1<<40-1)
	case PolicyTTL:
		if e.expireAt != 0 {
			// Keys with a TTL always rank before persistent keys.
			return e.expireAt - (1 << 62)
		}
		return e.access.Load()
	case PolicyRandom:
		return 0
	default:
		return e.access.Load()
	}
}

// lfuIncr increments the logarithmic frequency counter: the higher the
// counter, the less likely an access bumps it (same idea as Redis' LFU).
func lfuIncr(e *entry) {
	f := e.freq.Load()
	if f >= 255 {
		return
	}
	if f < 5 || rand.Float64() < 1/float64((f-4)*10+1) {
		e.freq.CompareAndSwap(f, f+1)
	}
}

// expireLoop actively removes expired keys and decays LFU counters.
func (c *BoltCache) expireLoop(interval time.Duration) {
	defer c.wg.Done()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-c.stop:
			return
		case <-ticker.C:
			c.DeleteExpired()
		}
	}
}

// DeleteExpired removes all expired keys and returns how many were removed.
func (c *BoltCache) DeleteExpired() int {
	ts := now()
	removed := 0
	decay := c.opts.EvictionPolicy == PolicyLFU && c.opts.limited()
	for i := range c.shards {
		s := &c.shards[i]
		s.mu.Lock()
		for k, e := range s.m {
			if e.expired(ts) {
				c.removeLocked(s, k, e)
				removed++
				continue
			}
			if decay {
				if f := e.freq.Load(); f > 0 {
					e.freq.Store(f / 2)
				}
			}
		}
		s.mu.Unlock()
	}
	if removed > 0 {
		c.expired.Add(uint64(removed))
	}
	return removed
}
