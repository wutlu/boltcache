package cache

import (
	"math/rand/v2"
	"sort"
	"time"

	"github.com/wutlu/boltcache/v2/internal/glob"
)

// Delete removes key and reports whether it existed.
func (t *Tx) Delete(key string) (bool, error) {
	existed := false
	err := t.update(key, func(e *entry) (*entry, error) {
		existed = e != nil
		return nil, nil
	})
	return existed, err
}

// Del removes keys and returns how many existed.
func (t *Tx) Del(keys ...string) (int, error) {
	n := 0
	for _, k := range keys {
		ok, err := t.Delete(k)
		if err != nil {
			return n, err
		}
		if ok {
			n++
		}
	}
	return n, nil
}

// Exists returns how many of keys exist (duplicates are counted twice, like
// Redis).
func (t *Tx) Exists(keys ...string) (int, error) {
	n := 0
	for _, k := range keys {
		err := t.view(k, func(e *entry) error {
			if e != nil {
				n++
			}
			return nil
		})
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

// Type returns the kind of value at key (KindNone when absent).
func (t *Tx) Type(key string) (Kind, error) {
	k := KindNone
	err := t.view(key, func(e *entry) error {
		if e != nil {
			k = e.kind
		}
		return nil
	})
	return k, err
}

// ExpireCond restricts when an expiration is applied (EXPIRE NX|XX|GT|LT).
type ExpireCond uint8

const (
	ExpireAlways ExpireCond = iota
	ExpireNX                // only if the key has no expiry
	ExpireXX                // only if the key has an expiry
	ExpireGT                // only if the new expiry is greater
	ExpireLT                // only if the new expiry is less
)

// ExpireAt sets key's absolute expiration time. A time in the past deletes
// the key. It reports whether the timeout was set.
func (t *Tx) ExpireAt(key string, at time.Time, cond ExpireCond) (bool, error) {
	set := false
	ts := unixNanoClamped(at)
	err := t.update(key, func(e *entry) (*entry, error) {
		if e == nil {
			return nil, nil
		}
		switch cond {
		case ExpireNX:
			if e.expireAt != 0 {
				return e, nil
			}
		case ExpireXX:
			if e.expireAt == 0 {
				return e, nil
			}
		case ExpireGT:
			// A persistent key counts as an infinite TTL.
			if e.expireAt == 0 || ts <= e.expireAt {
				return e, nil
			}
		case ExpireLT:
			if e.expireAt != 0 && ts >= e.expireAt {
				return e, nil
			}
		}
		set = true
		if ts <= now() {
			return nil, nil
		}
		e.expireAt = ts
		return e, nil
	})
	return set, err
}

// Expire sets a relative timeout on key.
func (t *Tx) Expire(key string, d time.Duration, cond ExpireCond) (bool, error) {
	return t.ExpireAt(key, time.Now().Add(d), cond)
}

// Persist removes key's timeout and reports whether one was removed.
func (t *Tx) Persist(key string) (bool, error) {
	removed := false
	err := t.update(key, func(e *entry) (*entry, error) {
		if e == nil {
			return nil, nil
		}
		if e.expireAt != 0 {
			e.expireAt = 0
			removed = true
		}
		return e, nil
	})
	return removed, err
}

// TTL states returned alongside a zero duration.
const (
	TTLNoKey    = -2 // key does not exist
	TTLNoExpire = -1 // key exists without a timeout
)

// TTL returns the remaining time to live of key. When the key is missing or
// has no timeout, the duration is 0 and state is TTLNoKey/TTLNoExpire;
// otherwise state is 0.
func (t *Tx) TTL(key string) (d time.Duration, state int, err error) {
	state = TTLNoKey
	err = t.view(key, func(e *entry) error {
		if e == nil {
			return nil
		}
		if e.expireAt == 0 {
			state = TTLNoExpire
			return nil
		}
		state = 0
		d = time.Duration(e.expireAt - now())
		if d < 0 {
			d = 0
		}
		return nil
	})
	return
}

// ExpireTime returns key's absolute expiration (zero time when none) with the
// same state convention as TTL.
func (t *Tx) ExpireTime(key string) (at time.Time, state int, err error) {
	state = TTLNoKey
	err = t.view(key, func(e *entry) error {
		if e == nil {
			return nil
		}
		if e.expireAt == 0 {
			state = TTLNoExpire
			return nil
		}
		state = 0
		at = time.Unix(0, e.expireAt)
		return nil
	})
	return
}

// Keys returns all live keys matching pattern, sorted.
func (t *Tx) Keys(pattern string) ([]string, error) {
	var out []string
	ts := now()
	literal := glob.IsLiteral(pattern)
	err := t.eachShard(false, func(s *shard) bool {
		if literal {
			if e, ok := s.m[pattern]; ok && !e.expired(ts) {
				out = append(out, pattern)
			}
			return true
		}
		for k, e := range s.m {
			if !e.expired(ts) && glob.Match(pattern, k) {
				out = append(out, k)
			}
		}
		return true
	})
	sort.Strings(out)
	return out, err
}

// Scan iterates the keyspace incrementally. The cursor is a shard index: a
// full iteration (starting and ending at cursor 0) returns every key that
// existed for the whole iteration at least once, matching SCAN's guarantee.
// kind filters by type when not KindNone.
func (t *Tx) Scan(cursor uint64, pattern string, count int, kind Kind) (next uint64, keys []string, err error) {
	if t.atomic && !t.all {
		return 0, nil, ErrNotInAtomicCtx
	}
	if count <= 0 {
		count = 10
	}
	if cursor >= shardCount { // not a cursor we handed out: iteration is over
		return 0, nil, nil
	}
	ts := now()
	i := int(cursor)
	for ; i < shardCount && len(keys) < count; i++ {
		t.withShard(i, false, func(s *shard) {
			for k, e := range s.m {
				if e.expired(ts) {
					continue
				}
				if kind != KindNone && e.kind != kind {
					continue
				}
				if pattern != "" && pattern != "*" && !glob.Match(pattern, k) {
					continue
				}
				keys = append(keys, k)
			}
		})
	}
	if i >= shardCount {
		return 0, keys, nil
	}
	return uint64(i), keys, nil
}

// RandomKey returns a random live key, or "" and false when empty.
func (t *Tx) RandomKey() (string, bool, error) {
	if t.atomic && !t.all {
		return "", false, ErrNotInAtomicCtx
	}
	ts := now()
	start := rand.IntN(shardCount)
	for n := 0; n < shardCount; n++ {
		var found string
		ok := false
		t.withShard((start+n)%shardCount, false, func(s *shard) {
			for k, e := range s.m {
				if !e.expired(ts) {
					found, ok = k, true
					break
				}
			}
		})
		if ok {
			return found, true, nil
		}
	}
	return "", false, nil
}

// FlushAll removes every key.
func (t *Tx) FlushAll() error {
	c := t.c
	return t.eachShard(true, func(s *shard) bool {
		if len(s.m) == 0 {
			return true
		}
		var n, mem int64
		for _, e := range s.m {
			n++
			mem += e.size
		}
		s.m = make(map[string]*entry)
		c.account(s, -n, -mem)
		s.writes.Add(1)
		return true
	})
}

// DBSize returns the number of live keys.
func (t *Tx) DBSize() (int64, error) {
	return t.c.Len(), nil
}

// Rename moves the value at src to dst, overwriting dst. With nx it fails
// (returning false) when dst exists. Both keys must be accessible in the
// current context; outside Atomic the two shards are locked in order so the
// move is atomic.
func (t *Tx) Rename(src, dst string, nx bool) (bool, error) {
	if t.atomic {
		return t.renameLocked(src, dst, nx)
	}
	var ok bool
	err := t.c.Atomic([]string{src, dst}, false, func(tx *Tx) error {
		var err error
		ok, err = tx.renameLocked(src, dst, nx)
		return err
	})
	return ok, err
}

func (t *Tx) renameLocked(src, dst string, nx bool) (bool, error) {
	var moved *entry
	err := t.view(src, func(e *entry) error {
		if e == nil {
			return ErrNoSuchKey
		}
		moved = e
		return nil
	})
	if err != nil {
		return false, err
	}
	if src == dst {
		return !nx, nil
	}
	// Check dst before touching src: inside a script dst may be undeclared,
	// and failing after deleting src would lose the value.
	n, err := t.Exists(dst)
	if err != nil {
		return false, err
	}
	if nx && n > 0 {
		return false, nil
	}
	if _, err := t.Delete(src); err != nil {
		return false, err
	}
	keyDelta := int64(len(dst) - len(src))
	err = t.update(dst, func(*entry) (*entry, error) {
		moved.size += keyDelta
		return moved, nil
	})
	return err == nil, err
}

// MemoryUsage returns the approximate bytes used by key (0 when absent).
func (t *Tx) MemoryUsage(key string) (int64, error) {
	var n int64
	err := t.view(key, func(e *entry) error {
		if e != nil {
			n = e.size
		}
		return nil
	})
	return n, err
}
