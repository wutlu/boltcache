package cache

import (
	"math"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unsafe"
)

func newString(key, val string) *entry {
	return &entry{kind: KindString, str: val}
}

func (e *entry) setString(val string) { e.str = val }

// Get returns the string value at key.
func (t *Tx) Get(key string) (string, bool, error) {
	_, i, r, unlock, err := t.readKey(key)
	if err != nil {
		return "", false, err
	}
	defer unlock()
	if i < 0 {
		return "", false, nil
	}
	if r.kind != KindString {
		return "", false, ErrWrongType
	}
	return strings.Clone(r.str), true, nil
}

// ValueWriter receives a value read by GetTo; resp.Writer implements it.
type ValueWriter interface {
	Bulk(s string)
	Null()
}

// GetTo is Get that hands the value to w while the key is still locked,
// which avoids copying it (GET).
func (t *Tx) GetTo(key string, w ValueWriter) error {
	_, i, r, unlock, err := t.readKey(key)
	if err != nil {
		return err
	}
	defer unlock()
	switch {
	case i < 0:
		w.Null()
	case r.kind != KindString:
		return ErrWrongType
	default:
		w.Bulk(r.str)
	}
	return nil
}

// SetOptions mirrors the options of the Redis SET command.
type SetOptions struct {
	TTL      time.Duration // relative expiration; 0 = none
	ExpireAt time.Time     // absolute expiration; takes precedence over TTL
	KeepTTL  bool          // retain the existing expiration
	NX       bool          // only set if the key does not exist
	XX       bool          // only set if the key exists
}

func (o SetOptions) expireAt() int64 {
	if !o.ExpireAt.IsZero() {
		return unixNanoClamped(o.ExpireAt)
	}
	if o.TTL > 0 {
		return time.Now().Add(o.TTL).UnixNano()
	}
	return 0
}

// SetResult reports the outcome of SetWithOptions.
type SetResult struct {
	Written bool   // false when NX/XX prevented the write
	Old     string // previous string value
	HadOld  bool   // a previous string value existed
}

// Set stores a string value, replacing any existing value and type. A ttl of
// 0 means no expiration.
func (t *Tx) Set(key, val string, ttl time.Duration) error {
	_, err := t.SetWithOptions(key, val, SetOptions{TTL: ttl}, false)
	return err
}

// SetWithOptions implements SET with NX/XX/KEEPTTL/EX/PX. When wantOld is
// true the previous value is returned (SET ... GET), which fails with
// ErrWrongType if the old value isn't a string.
func (t *Tx) SetWithOptions(key, val string, opts SetOptions, wantOld bool) (SetResult, error) {
	var res SetResult
	c := t.c
	if !t.atomic && c.OOM() {
		return res, ErrOOM
	}
	exp := opts.expireAt()
	s, h, i, r, unlock, err := t.writeKey(key)
	if err != nil {
		return res, err
	}
	exists := i >= 0
	if exists && wantOld {
		if r.kind != KindString {
			unlock()
			return res, ErrWrongType
		}
		res.Old, res.HadOld = strings.Clone(r.str), true
	}
	if (opts.NX && exists) || (opts.XX && !exists) {
		unlock()
		return res, nil
	}
	res.Written = true
	var oldSize int64
	if exists {
		if opts.KeepTTL {
			exp = r.expireAt
			if r.kind != KindString {
				exp = s.t.coll(r).expireAt
			}
		}
		oldSize = s.memSizeAt(i, key)
	}
	var oldMeta *atomic.Uint64
	if exists && r.kind == KindString {
		oldMeta = r.meta
	}
	c.storeString(s, key, h, val, exp, oldMeta)
	c.account(s, b2i(!exists), stringSize(key, val)-oldSize)
	s.bumpWrites()
	unlock()
	if !t.atomic {
		c.maybeEvict()
	}
	return res, nil
}

// MSet stores pairs (key, value, key, value...) of plain strings, like
// Set with no TTL for each pair. Inside Atomic, where every key's shard is
// already held, it skips Set's per-key machinery: MSET writes many keys and
// that overhead dominated it.
func (t *Tx) MSet(pairs []string) error {
	if !t.atomic {
		for i := 0; i+1 < len(pairs); i += 2 {
			if err := t.Set(pairs[i], pairs[i+1], 0); err != nil {
				return err
			}
		}
		return nil
	}
	c := t.c
	// Check every key first so an undeclared one doesn't leave MSET half
	// done.
	for i := 0; i+1 < len(pairs); i += 2 {
		if !t.holds(c.shardIndex(pairs[i])) {
			return ErrUndeclaredKey
		}
	}
	for i := 0; i+1 < len(pairs); i += 2 {
		key, val := pairs[i], pairs[i+1]
		h := c.hash(key)
		s := &c.shards[h&(shardCount-1)]
		var oldSize int64
		var oldMeta *atomic.Uint64
		added := int64(1)
		if j := s.t.find(key, h); j >= 0 {
			oldSize, added = s.memSizeAt(j, key), 0
			if r := s.t.decode(s.t.slots[j]); r.kind == KindString {
				oldMeta = r.meta
			}
		}
		c.storeString(s, key, h, val, 0, oldMeta)
		c.account(s, added, stringSize(key, val)-oldSize)
		s.bumpWrites()
	}
	return nil
}

// MGet returns the values for keys; missing or non-string keys yield ok=false.
func (t *Tx) MGet(keys ...string) ([]string, []bool, error) {
	vals := make([]string, len(keys))
	oks := make([]bool, len(keys))
	for i, k := range keys {
		_, j, r, unlock, err := t.readKey(k)
		if err != nil {
			return nil, nil, err
		}
		if j >= 0 && r.kind == KindString {
			vals[i], oks[i] = strings.Clone(r.str), true
		}
		unlock()
	}
	return vals, oks, nil
}

// GetDel returns the string at key and deletes it.
func (t *Tx) GetDel(key string) (string, bool, error) {
	var (
		val string
		ok  bool
	)
	err := t.update(key, func(e *entry) (*entry, error) {
		if e == nil {
			return nil, nil
		}
		if e.kind != KindString {
			return nil, ErrWrongType
		}
		val, ok = e.str, true
		return nil, nil
	})
	return val, ok, err
}

// GetEx returns the string at key and optionally changes its expiration:
// exp > 0 sets an absolute unix-nano expiry, persist removes it.
func (t *Tx) GetEx(key string, exp int64, persist bool) (string, bool, error) {
	var (
		val string
		ok  bool
	)
	err := t.update(key, func(e *entry) (*entry, error) {
		if e == nil {
			return nil, nil
		}
		if e.kind != KindString {
			return nil, ErrWrongType
		}
		val, ok = e.str, true
		switch {
		case persist:
			e.expireAt = 0
		case exp > 0:
			if exp <= now() {
				return nil, nil
			}
			e.expireAt = exp
		}
		return e, nil
	})
	return val, ok, err
}

// IncrBy adds delta to the integer stored at key (0 when absent) and returns
// the new value. The key's TTL is preserved.
func (t *Tx) IncrBy(key string, delta int64) (int64, error) {
	c := t.c
	if !t.atomic && c.OOM() {
		return 0, ErrOOM
	}
	s, h, i, r, unlock, err := t.writeKey(key)
	if err != nil {
		return 0, err
	}
	result, err := c.incrLocked(s, key, h, i, r, delta)
	unlock()
	if err == nil && !t.atomic {
		c.maybeEvict()
	}
	return result, err
}

func (c *BoltCache) incrLocked(s *shard, key string, h uint64, i int, r rec, delta int64) (int64, error) {
	var cur int64
	if i >= 0 {
		if r.kind != KindString {
			return 0, ErrWrongType
		}
		n, err := strconv.ParseInt(r.str, 10, 64)
		if err != nil {
			return 0, ErrNotInteger
		}
		cur = n
	}
	if (delta > 0 && cur > math.MaxInt64-delta) || (delta < 0 && cur < math.MinInt64-delta) {
		return 0, ErrOverflow
	}
	result := cur + delta
	var buf [20]byte
	b := strconv.AppendInt(buf[:0], result, 10)
	val := unsafe.String(unsafe.SliceData(b), len(b)) // copied by storeString
	var oldSize int64
	if i >= 0 {
		oldSize = stringSize(key, r.str)
	}
	c.storeString(s, key, h, val, r.expireAt, r.meta)
	c.account(s, b2i(i < 0), stringSize(key, val)-oldSize)
	s.bumpWrites()
	return result, nil
}

// IncrByFloat adds delta to the float stored at key.
func (t *Tx) IncrByFloat(key string, delta float64) (float64, error) {
	var result float64
	err := t.write(key, func(e *entry) (*entry, error) {
		var cur float64
		if e != nil {
			if e.kind != KindString {
				return nil, ErrWrongType
			}
			f, err := strconv.ParseFloat(e.str, 64)
			if err != nil {
				return nil, ErrNotFloat
			}
			cur = f
		}
		result = cur + delta
		if math.IsNaN(result) || math.IsInf(result, 0) {
			return nil, ErrNotFloat
		}
		s := strconv.FormatFloat(result, 'f', -1, 64)
		if e == nil {
			return newString(key, s), nil
		}
		e.setString(s)
		return e, nil
	})
	return result, err
}

// Append appends val to the string at key and returns the new length.
func (t *Tx) Append(key, val string) (int, error) {
	var n int
	err := t.write(key, func(e *entry) (*entry, error) {
		if e == nil {
			n = len(val)
			return newString(key, val), nil
		}
		if e.kind != KindString {
			return nil, ErrWrongType
		}
		e.setString(e.str + val)
		n = len(e.str)
		return e, nil
	})
	return n, err
}

// StrLen returns the length of the string at key.
func (t *Tx) StrLen(key string) (int, error) {
	ki, ok, err := t.peek(key)
	if err != nil || !ok {
		return 0, err
	}
	if ki.kind != KindString {
		return 0, ErrWrongType
	}
	return ki.strLen, nil
}

// GetRange returns the substring of the string at key between start and end
// (inclusive, negative offsets count from the end).
func (t *Tx) GetRange(key string, start, end int) (string, error) {
	_, i, r, unlock, err := t.readKey(key)
	if err != nil {
		return "", err
	}
	defer unlock()
	if i < 0 {
		return "", nil
	}
	if r.kind != KindString {
		return "", ErrWrongType
	}
	if s, en, ok := clampRange(start, end, len(r.str)); ok {
		return strings.Clone(r.str[s : en+1]), nil // copy only the range
	}
	return "", nil
}

// clampRange converts Redis-style inclusive (possibly negative) indexes into
// valid [start, end] bounds for a sequence of length n.
func clampRange(start, end, n int) (int, int, bool) {
	if start < 0 {
		start += n
	}
	if end < 0 {
		end += n
	}
	if start < 0 {
		start = 0
	}
	if end >= n {
		end = n - 1
	}
	if start > end || n == 0 {
		return 0, 0, false
	}
	return start, end, true
}
