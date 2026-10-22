package cache

import (
	"errors"
	"math"
	"sort"
	"strconv"
)

func newHash(key string) *entry {
	return &entry{kind: KindHash, hash: make(map[string]string), size: entryOverhead + int64(len(key))}
}

func hashFieldSize(f, v string) int64 { return hashOverhead + int64(len(f)+len(v)) }

// HSet sets field/value pairs (pairs must have even length) and returns how
// many fields were newly created.
func (t *Tx) HSet(key string, pairs ...string) (int, error) {
	return t.hset(key, pairs, false)
}

// HSetNX sets field only if it does not exist.
func (t *Tx) HSetNX(key, field, val string) (bool, error) {
	n, err := t.hset(key, []string{field, val}, true)
	return n == 1, err
}

func (t *Tx) hset(key string, pairs []string, nx bool) (int, error) {
	created := 0
	err := t.write(key, func(e *entry) (*entry, error) {
		if e == nil {
			e = newHash(key)
		} else if e.kind != KindHash {
			return nil, ErrWrongType
		}
		for i := 0; i+1 < len(pairs); i += 2 {
			f, v := pairs[i], pairs[i+1]
			old, exists := e.hash[f]
			if exists {
				if nx {
					continue
				}
				e.size += int64(len(v) - len(old))
			} else {
				e.size += hashFieldSize(f, v)
				created++
			}
			e.hash[f] = v
		}
		if len(e.hash) == 0 {
			return nil, nil
		}
		return e, nil
	})
	return created, err
}

func (t *Tx) viewHash(key string, fn func(h map[string]string)) error {
	return t.view(key, func(e *entry) error {
		if e == nil {
			return nil
		}
		if e.kind != KindHash {
			return ErrWrongType
		}
		fn(e.hash)
		return nil
	})
}

// HGet returns the value of field.
func (t *Tx) HGet(key, field string) (string, bool, error) {
	var (
		v  string
		ok bool
	)
	err := t.viewHash(key, func(h map[string]string) { v, ok = h[field] })
	return v, ok, err
}

// HMGet returns values for fields.
func (t *Tx) HMGet(key string, fields ...string) ([]string, []bool, error) {
	vals := make([]string, len(fields))
	oks := make([]bool, len(fields))
	err := t.viewHash(key, func(h map[string]string) {
		for i, f := range fields {
			vals[i], oks[i] = h[f]
		}
	})
	return vals, oks, err
}

// HGetAll returns field/value pairs sorted by field.
func (t *Tx) HGetAll(key string) ([]string, error) {
	var out []string
	err := t.viewHash(key, func(h map[string]string) {
		fields := make([]string, 0, len(h))
		for f := range h {
			fields = append(fields, f)
		}
		sort.Strings(fields)
		out = make([]string, 0, 2*len(h))
		for _, f := range fields {
			out = append(out, f, h[f])
		}
	})
	if out == nil {
		out = []string{}
	}
	return out, err
}

// HKeys returns the hash's fields, sorted.
func (t *Tx) HKeys(key string) ([]string, error) {
	pairs, err := t.HGetAll(key)
	out := make([]string, 0, len(pairs)/2)
	for i := 0; i < len(pairs); i += 2 {
		out = append(out, pairs[i])
	}
	return out, err
}

// HVals returns the hash's values ordered by field.
func (t *Tx) HVals(key string) ([]string, error) {
	pairs, err := t.HGetAll(key)
	out := make([]string, 0, len(pairs)/2)
	for i := 1; i < len(pairs); i += 2 {
		out = append(out, pairs[i])
	}
	return out, err
}

// HDel removes fields and returns how many existed.
func (t *Tx) HDel(key string, fields ...string) (int, error) {
	removed := 0
	err := t.update(key, func(e *entry) (*entry, error) {
		if e == nil {
			return nil, nil
		}
		if e.kind != KindHash {
			return nil, ErrWrongType
		}
		for _, f := range fields {
			if v, ok := e.hash[f]; ok {
				delete(e.hash, f)
				e.size -= hashFieldSize(f, v)
				removed++
			}
		}
		if len(e.hash) == 0 {
			return nil, nil
		}
		return e, nil
	})
	return removed, err
}

// HExists reports whether field exists.
func (t *Tx) HExists(key, field string) (bool, error) {
	_, ok, err := t.HGet(key, field)
	return ok, err
}

// HLen returns the number of fields.
func (t *Tx) HLen(key string) (int, error) {
	n := 0
	err := t.viewHash(key, func(h map[string]string) { n = len(h) })
	return n, err
}

// HStrLen returns the length of a field's value.
func (t *Tx) HStrLen(key, field string) (int, error) {
	n := 0
	err := t.viewHash(key, func(h map[string]string) { n = len(h[field]) })
	return n, err
}

// HIncrBy adds delta to the integer in field.
func (t *Tx) HIncrBy(key, field string, delta int64) (int64, error) {
	var result int64
	err := t.write(key, func(e *entry) (*entry, error) {
		if e == nil {
			e = newHash(key)
		} else if e.kind != KindHash {
			return nil, ErrWrongType
		}
		var cur int64
		old, exists := e.hash[field]
		if exists {
			n, err := strconv.ParseInt(old, 10, 64)
			if err != nil {
				return nil, errHashNotInteger
			}
			cur = n
		}
		if (delta > 0 && cur > math.MaxInt64-delta) || (delta < 0 && cur < math.MinInt64-delta) {
			return nil, ErrOverflow
		}
		result = cur + delta
		e.setHashField(field, strconv.FormatInt(result, 10), old, exists)
		return e, nil
	})
	return result, err
}

// HIncrByFloat adds delta to the float in field.
func (t *Tx) HIncrByFloat(key, field string, delta float64) (float64, error) {
	var result float64
	err := t.write(key, func(e *entry) (*entry, error) {
		if e == nil {
			e = newHash(key)
		} else if e.kind != KindHash {
			return nil, ErrWrongType
		}
		var cur float64
		old, exists := e.hash[field]
		if exists {
			f, err := strconv.ParseFloat(old, 64)
			if err != nil {
				return nil, ErrNotFloat
			}
			cur = f
		}
		result = cur + delta
		if math.IsNaN(result) || math.IsInf(result, 0) {
			return nil, ErrNotFloat
		}
		e.setHashField(field, strconv.FormatFloat(result, 'f', -1, 64), old, exists)
		return e, nil
	})
	return result, err
}

func (e *entry) setHashField(field, val, old string, exists bool) {
	if exists {
		e.size += int64(len(val) - len(old))
	} else {
		e.size += hashFieldSize(field, val)
	}
	e.hash[field] = val
}

var errHashNotInteger = errors.New("ERR hash value is not an integer")
