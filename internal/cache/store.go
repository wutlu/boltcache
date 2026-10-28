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
	"slices"
	"sort"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
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
	KindZSet
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
	case KindZSet:
		return "zset"
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

// entry is one key's value. It's kept to 48 bytes (a Go size class) since
// there's one per key: collection payloads live behind col, and the LRU
// clock and LFU counter share one word.
type entry struct {
	kind     Kind
	str      string // KindString value
	col      *coll  // other kinds
	expireAt int64  // unix nanoseconds, 0 = persistent

	// meta holds the last access time in lruUnit ticks (LRU) in the low 56
	// bits and the logarithmic access counter (LFU) in the high 8.
	meta atomic.Uint64
}

// coll is the payload of a collection value.
type coll struct {
	size int64 // approximate bytes, including key and entry
	list *deque
	set  map[string]struct{}
	hash map[string]string
	zset *zset
}

// memSize returns the approximate bytes used by key and e.
func (e *entry) memSize(key string) int64 {
	if e.col != nil {
		return e.col.size
	}
	return entryOverhead + int64(len(key)+len(e.str))
}

const (
	lruShift  = 20 // LRU clock unit: 2^20ns ~ 1ms
	clockMask = 1<<56 - 1
)

func (e *entry) accessed() int64 { return int64(e.meta.Load() & clockMask) }
func (e *entry) freq() uint64    { return e.meta.Load() >> 56 }

// setAccess records an access at unix nanoseconds ts, keeping the counter.
// Concurrent readers may race; losing an update only makes the policy a
// little less precise (Redis' LRU clock is approximate too).
func (e *entry) setAccess(ts int64) {
	m := e.meta.Load()
	e.meta.Store(m&^clockMask | uint64(ts>>lruShift)&clockMask)
}

func (e *entry) expired(now int64) bool {
	return e.expireAt != 0 && now >= e.expireAt
}

// expiredNow is expired(now()) that only reads the clock for keys with a
// TTL: most keys have none, and time.Now showed up in profiles.
func (e *entry) expiredNow() bool {
	return e.expireAt != 0 && now() >= e.expireAt
}

// shard counters are per shard so hot paths never contend on a global
// cache line. keys/mem/writes change only under the write lock, so they're
// updated with a load and a store instead of an atomic add (see rwLock);
// they're atomic because readers outside the lock (INFO, WATCH) load them.
// hits/misses are bumped by concurrent readers.
type shard struct {
	mu     rwLock
	m      map[string]*entry
	keys   atomic.Int64
	mem    atomic.Int64
	writes atomic.Uint64
	hits   atomic.Uint64
	misses atomic.Uint64
	_      [64]byte // keep hot mutexes on separate cache lines
}

func (s *shard) lock()  { s.mu.Lock() }
func (s *shard) rlock() { s.mu.RLock() }

// bumpWrites counts a mutation; callers hold the write lock.
func (s *shard) bumpWrites() { s.writes.Store(s.writes.Load() + 1) }

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
func (c *BoltCache) touch(e *entry) {
	if !c.opts.limited() {
		return
	}
	e.setAccess(now())
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
		s.keys.Store(s.keys.Load() + keys)
	}
	if mem != 0 {
		s.mem.Store(s.mem.Load() + mem)
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

	heldBuf [16]int // backing store for small held sets
}

func (t *Tx) holds(i int) bool {
	if t.all {
		return true
	}
	if len(t.held) <= 16 {
		for _, h := range t.held {
			if h >= i {
				return h == i
			}
		}
		return false
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
	t.c.shards[i].rlock()
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
	t.c.shards[i].lock()
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
	s := &c.shards[i]
	e := s.m[key]
	if e != nil && e.expiredNow() {
		e = nil
	}
	if e != nil {
		s.hits.Add(1)
		c.touch(e)
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
	e := s.m[key]
	if e != nil && e.expiredNow() {
		c.removeLocked(s, key, e)
		c.expired.Add(1)
		e = nil
	}
	var oldSize int64
	if e != nil {
		oldSize = e.memSize(key)
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
		c.account(s, 1, ne.memSize(key))
		c.touch(ne)
	default:
		// Re-storing the key when it shares an allocation with the new
		// string value (see adjacent) releases the one holding the old
		// value, which the map key would otherwise keep alive.
		if ne != e || (ne.kind == KindString && adjacent(key, ne.str)) {
			s.m[key] = ne
		}
		c.account(s, 0, ne.memSize(key)-oldSize)
		c.touch(ne)
	}
	s.bumpWrites()
	return nil
}

// adjacent reports whether val starts right where key ends, i.e. both are
// slices of one allocation, as the RESP server passes SET's arguments.
func adjacent(key, val string) bool {
	return len(val) > 0 &&
		uintptr(unsafe.Pointer(unsafe.StringData(key)))+uintptr(len(key)) == uintptr(unsafe.Pointer(unsafe.StringData(val)))
}

func (c *BoltCache) removeLocked(s *shard, key string, e *entry) {
	delete(s.m, key)
	c.account(s, -1, -e.memSize(key))
	s.bumpWrites()
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
	tx := c.Begin(keys, all)
	// End unlocks even if fn panics, so a faulty script can't wedge the
	// keyspace.
	defer tx.End()
	return fn(tx)
}

var txPool = sync.Pool{New: func() any { return new(Tx) }}

// Begin is Atomic without a callback, for hot paths where the closure would
// cost an allocation: it locks the shards owning keys (or all shards) and
// returns their handle. The caller must call End exactly once, typically
// deferred, and must not use the Tx afterwards.
func (c *BoltCache) Begin(keys []string, all bool) *Tx {
	tx := txPool.Get().(*Tx)
	tx.c, tx.atomic, tx.all = c, true, all
	if all {
		for i := range c.shards {
			c.shards[i].mu.Lock()
		}
		return tx
	}
	idx := tx.heldBuf[:0]
	for _, k := range keys {
		idx = append(idx, c.shardIndex(k))
	}
	slices.Sort(idx)
	tx.held = slices.Compact(idx)
	for _, i := range tx.held {
		c.shards[i].lock()
	}
	return tx
}

// TryBeginKey is Begin for a single key that returns nil instead of
// waiting when the key's shard is locked.
func (c *BoltCache) TryBeginKey(key string) *Tx {
	i := c.shardIndex(key)
	if !c.shards[i].mu.TryLock() {
		return nil
	}
	tx := txPool.Get().(*Tx)
	tx.c, tx.atomic, tx.all = c, true, false
	tx.held = append(tx.heldBuf[:0], i)
	return tx
}

// End releases the locks taken by Begin and runs eviction if needed.
func (t *Tx) End() {
	c := t.c
	if t.all {
		for i := len(c.shards) - 1; i >= 0; i-- {
			c.shards[i].mu.Unlock()
		}
	} else {
		for j := len(t.held) - 1; j >= 0; j-- {
			c.shards[t.held[j]].mu.Unlock()
		}
	}
	*t = Tx{}
	txPool.Put(t)
	c.maybeEvict()
}

// InAtomic reports whether t is the handle of an Atomic callback.
func (t *Tx) InAtomic() bool { return t.atomic }

// ShardCount returns the number of shards (the range of ShardOf).
func (c *BoltCache) ShardCount() int { return shardCount }

// ShardOf returns the index of the shard holding key; keys with the same
// index share a lock.
func (c *BoltCache) ShardOf(key string) int { return c.shardIndex(key) }

// KeyVersion returns a counter that changes whenever key (or another key in
// the same shard) is modified. Used to implement WATCH.
func (c *BoltCache) KeyVersion(key string) uint64 {
	return c.shards[c.shardIndex(key)].writes.Load()
}
