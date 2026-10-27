package cache

import (
	"math"
	"strconv"
	"time"
)

func newString(key, val string) *entry {
	return &entry{kind: KindString, str: val, size: entryOverhead + int64(len(key)+len(val))}
}

func (e *entry) setString(val string) {
	e.size += int64(len(val) - len(e.str))
	e.str = val
}

// Get returns the string value at key.
func (t *Tx) Get(key string) (string, bool, error) {
	var (
		val string
		ok  bool
	)
	err := t.view(key, func(e *entry) error {
		if e == nil {
			return nil
		}
		if e.kind != KindString {
			return ErrWrongType
		}
		val, ok = e.str, true
		return nil
	})
	return val, ok, err
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
	exp := opts.expireAt()
	err := t.write(key, func(e *entry) (*entry, error) {
		if e != nil && wantOld {
			if e.kind != KindString {
				return nil, ErrWrongType
			}
			res.Old, res.HadOld = e.str, true
		}
		if (opts.NX && e != nil) || (opts.XX && e == nil) {
			return e, nil
		}
		res.Written = true
		if e != nil && e.kind == KindString {
			// Overwrite in place: avoids an allocation and a map write.
			e.setString(val)
			if !opts.KeepTTL {
				e.expireAt = exp
			}
			return e, nil
		}
		ne := newString(key, val)
		if opts.KeepTTL && e != nil {
			ne.expireAt = e.expireAt
		} else {
			ne.expireAt = exp
		}
		return ne, nil
	})
	return res, err
}

// MGet returns the values for keys; missing or non-string keys yield ok=false.
func (t *Tx) MGet(keys ...string) ([]string, []bool, error) {
	vals := make([]string, len(keys))
	oks := make([]bool, len(keys))
	for i, k := range keys {
		err := t.view(k, func(e *entry) error {
			if e != nil && e.kind == KindString {
				vals[i], oks[i] = e.str, true
			}
			return nil
		})
		if err != nil {
			return nil, nil, err
		}
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
	var result int64
	err := t.write(key, func(e *entry) (*entry, error) {
		var cur int64
		if e != nil {
			if e.kind != KindString {
				return nil, ErrWrongType
			}
			n, err := strconv.ParseInt(e.str, 10, 64)
			if err != nil {
				return nil, ErrNotInteger
			}
			cur = n
		}
		if (delta > 0 && cur > math.MaxInt64-delta) || (delta < 0 && cur < math.MinInt64-delta) {
			return nil, ErrOverflow
		}
		result = cur + delta
		s := strconv.FormatInt(result, 10)
		if e == nil {
			return newString(key, s), nil
		}
		e.setString(s)
		return e, nil
	})
	return result, err
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
	var n int
	err := t.view(key, func(e *entry) error {
		if e == nil {
			return nil
		}
		if e.kind != KindString {
			return ErrWrongType
		}
		n = len(e.str)
		return nil
	})
	return n, err
}

// GetRange returns the substring of the string at key between start and end
// (inclusive, negative offsets count from the end).
func (t *Tx) GetRange(key string, start, end int) (string, error) {
	var out string
	err := t.view(key, func(e *entry) error {
		if e == nil {
			return nil
		}
		if e.kind != KindString {
			return ErrWrongType
		}
		s, en, ok := clampRange(start, end, len(e.str))
		if ok {
			out = e.str[s : en+1]
		}
		return nil
	})
	return out, err
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
