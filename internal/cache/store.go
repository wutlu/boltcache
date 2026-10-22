// Package cache implements BoltCache's in-memory keyspace: typed values
// (strings, lists, sets, hashes), expiration, memory/key limits with eviction,
// multi-key atomic transactions and snapshot persistence.
//
// Concurrency model: the keyspace is split into shards, each guarded by its
// own RWMutex. Every operation on a key runs entirely under that key's shard
// lock, so single-key commands are atomic. Multi-key atomicity (MULTI/EXEC and
// Lua scripts) is provided by Atomic, which locks all involved shards in a
// fixed order before running.
package cache

import (
	"errors"
	"hash/maphash"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

const shardCount = 1024 // must be a power of two

// Kind is the type of value stored at a key.
type Kind uint8

const (
	KindNone Kind = iota
	KindString
	KindList
	KindSet
	KindHash
)

func (k Kind) String() string {
	switch k {
	case KindString:
		return "string"
	case KindList:
		return "list"
	case KindSet:
		return "set"
	case KindHash:
		return "hash"
	}
	return "none"
}

// Errors returned by cache operations. Their text follows Redis conventions
// so they can be sent to RESP clients verbatim.
var (
	ErrWrongType      = errors.New("WRONGTYPE Operation against a key holding the wrong kind of value")
	ErrNotInteger     = errors.New("ERR value is not an integer or out of range")
	ErrNotFloat       = errors.New("ERR value is not a valid float")
	ErrOverflow       = errors.New("ERR increment or decrement would overflow")
	ErrNoSuchKey      = errors.New("ERR no such key")
	ErrIndexRange     = errors.New("ERR index out of range")
	ErrOOM            = errors.New("OOM command not allowed when used memory > 'maxmemory'")
	ErrUndeclaredKey  = errors.New("ERR script tried accessing a key that was not declared in KEYS")
	ErrNotInAtomicCtx = errors.New("ERR this command is not allowed inside scripts")
)

// Approximate per-item memory overheads used for max_memory accounting.
const (
	entryOverhead   = 96
	elementOverhead = 16
	setOverhead     = 24
	hashOverhead    = 32
)

type entry struct {
	kind     Kind
	str      string
	list     *deque
	set      map[string]struct{}
	hash     map[string]string
	expireAt int64 // unix nanoseconds, 0 = persistent
	size     int64 // approximate bytes, including key

	access atomic.Int64  // last access, unix nanoseconds (LRU)
	freq   atomic.Uint32 // logarithmic access counter (LFU)
}

func (e *entry) expired(now int64) bool {
	return e.expireAt != 0 && now >= e.expireAt
}

// shard counters are per shard so hot paths never contend on a global
// cache line. keys/mem/writes change only under the write lock; hits/misses
// are bumped by concurrent readers.
type shard struct {
	mu     sync.RWMutex
	m      map[string]*entry
	keys   atomic.Int64
	mem    atomic.Int64
	writes atomic.Uint64
	hits   atomic.Uint64
	misses atomic.Uint64
	_      [64]byte // keep hot mutexes on separate cache lines
}

// Stats is a point-in-time view of cache counters.
type Stats struct {
	Keys        int64
	KeysWithTTL int64
	UsedMemory  int64
	Hits        uint64
	Misses      uint64
	Expired     uint64
	Evicted     uint64
	Writes      uint64
}

// BoltCache is the keyspace. Its data methods (Get, Set, LPush, ...) are
// promoted from an embedded non-transactional Tx.
type BoltCache struct {
	*Tx

	shards []shard
	seed   maphash.Seed
	opts   Options

	// Global totals, maintained only when a limit is configured so that
	// eviction checks are O(1).
	gkeys atomic.Int64
	gmem  atomic.Int64

	expired atomic.Uint64
	evicted atomic.Uint64

	stopOnce sync.Once
	stop     chan struct{}
	wg       sync.WaitGroup
}

// New creates a cache and starts its background expiry loop. Call Close to
// stop it.
func New(opts Options) *BoltCache {
	opts.normalize()
	c := &BoltCache{
		shards: make([]shard, shardCount),
		seed:   maphash.MakeSeed(),
		opts:   opts,
		stop:   make(chan struct{}),
	}
	for i := range c.shards {
		c.shards[i].m = make(map[string]*entry)
	}
	c.Tx = &Tx{c: c}

	if opts.CleanupInterval > 0 {
		c.wg.Add(1)
		go c.expireLoop(opts.CleanupInterval)
	}
	return c
}

// Close stops background goroutines. The data remains readable.
func (c *BoltCache) Close() {
	c.stopOnce.Do(func() { close(c.stop) })
	c.wg.Wait()
}

// Options returns the effective options.
func (c *BoltCache) Options() Options { return c.opts }

func (c *BoltCache) shardIndex(key string) int {
	return int(maphash.String(c.seed, key) & (shardCount - 1))
}

func now() int64 { return time.Now().UnixNano() }

// touch records an access for the eviction policies. It's skipped when no
// limit is configured so unbounded caches don't pay for it.
func (c *BoltCache) touch(e *entry, ts int64) {
	if !c.opts.limited() {
		return
	}
	e.access.Store(ts)
	if c.opts.EvictionPolicy == PolicyLFU {
		lfuIncr(e)
	}
}

// Stats returns current counters.
func (c *BoltCache) Stats() Stats {
	st := Stats{
		Expired: c.expired.Load(),
		Evicted: c.evicted.Load(),
	}
	for i := range c.shards {
		s := &c.shards[i]
		st.Keys += s.keys.Load()
		st.UsedMemory += s.mem.Load()
		st.Hits += s.hits.Load()
		st.Misses += s.misses.Load()
		st.Writes += s.writes.Load()
		s.mu.RLock()
		for _, e := range s.m {
			if e.expireAt != 0 {
				st.KeysWithTTL++
			}
		}
		s.mu.RUnlock()
	}
	return st
}

// Len returns the number of keys (including expired keys not yet reclaimed).
func (c *BoltCache) Len() int64 {
	var n int64
	for i := range c.shards {
		n += c.shards[i].keys.Load()
	}
	return n
}

// UsedMemory returns the approximate number of bytes held by the keyspace.
func (c *BoltCache) UsedMemory() int64 {
	var n int64
	for i := range c.shards {
		n += c.shards[i].mem.Load()
	}
	return n
}

// Writes returns a counter incremented on every mutation. Persistence uses it
// to skip snapshots when nothing changed.
func (c *BoltCache) Writes() uint64 {
	var n uint64
	for i := range c.shards {
		n += c.shards[i].writes.Load()
	}
	return n
}

// account adjusts key and memory counters of shard s; callers hold its write
// lock.
func (c *BoltCache) account(s *shard, keys, mem int64) {
	if keys != 0 {
		s.keys.Add(keys)
	}
	if mem != 0 {
		s.mem.Add(mem)
	}
	if c.opts.limited() {
		if keys != 0 {
			c.gkeys.Add(keys)
		}
		if mem != 0 {
			c.gmem.Add(mem)
		}
	}
}

// Tx is a handle for running operations. The cache's own embedded Tx locks
// shards per operation; a Tx passed to an Atomic callback already holds the
// locks for its declared keys.
type Tx struct {
	c      *BoltCache
	held   []int // sorted shard indices locked by Atomic; nil outside Atomic
	all    bool  // every shard is held
	atomic bool
}

func (t *Tx) holds(i int) bool {
	if t.all {
		return true
	}
	j := sort.SearchInts(t.held, i)
	return j < len(t.held) && t.held[j] == i
}

func (t *Tx) lockRead(i int) error {
	if t.atomic {
		if !t.holds(i) {
			return ErrUndeclaredKey
		}
		return nil
	}
	t.c.shards[i].mu.RLock()
	return nil
}

func (t *Tx) unlockRead(i int) {
	if !t.atomic {
		t.c.shards[i].mu.RUnlock()
	}
}

func (t *Tx) lockWrite(i int) error {
	if t.atomic {
		if !t.holds(i) {
			return ErrUndeclaredKey
		}
		return nil
	}
	t.c.shards[i].mu.Lock()
	return nil
}

func (t *Tx) unlockWrite(i int) {
	if !t.atomic {
		t.c.shards[i].mu.Unlock()
	}
}

// view runs fn with the live entry for key (nil when absent or expired)
// under a read lock. fn must not mutate the entry.
func (t *Tx) view(key string, fn func(e *entry) error) error {
	c := t.c
	i := c.shardIndex(key)
	if err := t.lockRead(i); err != nil {
		return err
	}
	defer t.unlockRead(i)
	ts := now()
	s := &c.shards[i]
	e := s.m[key]
	if e != nil && e.expired(ts) {
		e = nil
	}
	if e != nil {
		s.hits.Add(1)
		c.touch(e, ts)
	} else {
		s.misses.Add(1)
	}
	return fn(e)
}

// mutator receives the live entry for a key (nil when absent) and returns the
// entry that should be stored: the same pointer after an in-place change
// (with size updated), a new entry, or nil to delete the key. A mutator that
// returns an error must not have modified the entry.
type mutator func(e *entry) (*entry, error)

// update runs fn under the key's write lock and maintains key/memory
// accounting.
func (t *Tx) update(key string, fn mutator) error {
	return t.modify(key, false, fn)
}

// write is update for commands that may grow memory: with the noeviction
// policy it refuses to run once the memory/key limit is exceeded.
func (t *Tx) write(key string, fn mutator) error {
	return t.modify(key, true, fn)
}

func (t *Tx) modify(key string, grows bool, fn mutator) error {
	c := t.c
	// Inside Atomic the caller checked OOM before starting: refusing a later
	// step would leave a multi-step command (LMOVE, SMOVE...) half done.
	if grows && !t.atomic && c.OOM() {
		return ErrOOM
	}
	if err := t.modifyLocked(key, fn); err != nil {
		return err
	}
	if !t.atomic && grows {
		c.maybeEvict()
	}
	return nil
}

func (t *Tx) modifyLocked(key string, fn mutator) error {
	c := t.c
	i := c.shardIndex(key)
	if err := t.lockWrite(i); err != nil {
		return err
	}
	defer t.unlockWrite(i)
	s := &c.shards[i]
	ts := now()
	e := s.m[key]
	if e != nil && e.expired(ts) {
		c.removeLocked(s, key, e)
		c.expired.Add(1)
		e = nil
	}
	var oldSize int64
	if e != nil {
		oldSize = e.size
	}
	ne, err := fn(e)
	if err != nil {
		return err
	}
	switch {
	case ne == nil:
		if e != nil {
			delete(s.m, key)
			c.account(s, -1, -oldSize)
		}
	case e == nil:
		s.m[key] = ne
		c.account(s, 1, ne.size)
		c.touch(ne, ts)
	default:
		if ne != e {
			s.m[key] = ne
		}
		c.account(s, 0, ne.size-oldSize)
		c.touch(ne, ts)
	}
	s.writes.Add(1)
	return nil
}

func (c *BoltCache) removeLocked(s *shard, key string, e *entry) {
	delete(s.m, key)
	c.account(s, -1, -e.size)
	s.writes.Add(1)
}

// eachShard calls fn for every shard while holding its lock. Only allowed
// outside Atomic or when Atomic holds all shards.
func (t *Tx) eachShard(write bool, fn func(s *shard) bool) error {
	if t.atomic && !t.all {
		return ErrNotInAtomicCtx
	}
	for i := range t.c.shards {
		cont := true
		t.withShard(i, write, func(s *shard) { cont = fn(s) })
		if !cont {
			break
		}
	}
	return nil
}

// withShard runs fn on shard i, locking it unless the Atomic caller already
// holds every shard. The deferred unlock keeps a panic from leaking the lock.
func (t *Tx) withShard(i int, write bool, fn func(s *shard)) {
	s := &t.c.shards[i]
	if !t.atomic {
		if write {
			s.mu.Lock()
			defer s.mu.Unlock()
		} else {
			s.mu.RLock()
			defer s.mu.RUnlock()
		}
	}
	fn(s)
}

// Atomic runs fn with exclusive access to the shards owning keys (or to
// every shard when all is true). Operations on other keys inside fn fail
// with ErrUndeclaredKey. Shards are locked in ascending order, so concurrent
// Atomic calls cannot deadlock.
func (c *BoltCache) Atomic(keys []string, all bool, fn func(tx *Tx) error) (err error) {
	tx := &Tx{c: c, atomic: true, all: all}
	if all {
		for i := range c.shards {
			c.shards[i].mu.Lock()
		}
	} else {
		idx := make([]int, 0, len(keys))
		for _, k := range keys {
			idx = append(idx, c.shardIndex(k))
		}
		sort.Ints(idx)
		held := idx[:0]
		for j, v := range idx {
			if j == 0 || v != idx[j-1] {
				held = append(held, v)
			}
		}
		tx.held = held
		for _, i := range held {
			c.shards[i].mu.Lock()
		}
	}

	func() {
		// Unlock even if fn panics, so a faulty script can't wedge the keyspace.
		defer func() {
			if all {
				for i := len(c.shards) - 1; i >= 0; i-- {
					c.shards[i].mu.Unlock()
				}
			} else {
				for j := len(tx.held) - 1; j >= 0; j-- {
					c.shards[tx.held[j]].mu.Unlock()
				}
			}
		}()
		err = fn(tx)
	}()
	c.maybeEvict()
	return err
}

// InAtomic reports whether t is the handle of an Atomic callback.
func (t *Tx) InAtomic() bool { return t.atomic }

// KeyVersion returns a counter that changes whenever key (or another key in
// the same shard) is modified. Used to implement WATCH.
func (c *BoltCache) KeyVersion(key string) uint64 {
	return c.shards[c.shardIndex(key)].writes.Load()
}
