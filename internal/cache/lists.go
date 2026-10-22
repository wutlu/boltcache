package cache

func newList(key string) *entry {
	return &entry{kind: KindList, list: &deque{}, size: entryOverhead + int64(len(key))}
}

func listItemSize(v string) int64 { return elementOverhead + int64(len(v)) }

func (t *Tx) push(key string, vals []string, front, onlyIfExists bool) (int, error) {
	var n int
	err := t.write(key, func(e *entry) (*entry, error) {
		if e == nil {
			if onlyIfExists {
				return nil, nil
			}
			e = newList(key)
		} else if e.kind != KindList {
			return nil, ErrWrongType
		}
		for _, v := range vals {
			if front {
				e.list.PushFront(v)
			} else {
				e.list.PushBack(v)
			}
			e.size += listItemSize(v)
		}
		n = e.list.Len()
		return e, nil
	})
	return n, err
}

// LPush prepends values (each one becoming the new head, like Redis) and
// returns the list length.
func (t *Tx) LPush(key string, vals ...string) (int, error) { return t.push(key, vals, true, false) }

// RPush appends values and returns the list length.
func (t *Tx) RPush(key string, vals ...string) (int, error) { return t.push(key, vals, false, false) }

// LPushX is LPush that only acts on an existing list.
func (t *Tx) LPushX(key string, vals ...string) (int, error) { return t.push(key, vals, true, true) }

// RPushX is RPush that only acts on an existing list.
func (t *Tx) RPushX(key string, vals ...string) (int, error) { return t.push(key, vals, false, true) }

func (t *Tx) pop(key string, count int, front bool) ([]string, error) {
	var out []string
	err := t.update(key, func(e *entry) (*entry, error) {
		if e == nil {
			return nil, nil
		}
		if e.kind != KindList {
			return nil, ErrWrongType
		}
		for i := 0; i < count && e.list.Len() > 0; i++ {
			var v string
			if front {
				v = e.list.PopFront()
			} else {
				v = e.list.PopBack()
			}
			e.size -= listItemSize(v)
			out = append(out, v)
		}
		if e.list.Len() == 0 {
			return nil, nil
		}
		return e, nil
	})
	return out, err
}

// LPop removes and returns up to count elements from the head.
func (t *Tx) LPop(key string, count int) ([]string, error) { return t.pop(key, count, true) }

// RPop removes and returns up to count elements from the tail.
func (t *Tx) RPop(key string, count int) ([]string, error) { return t.pop(key, count, false) }

func (t *Tx) viewList(key string, fn func(d *deque)) error {
	return t.view(key, func(e *entry) error {
		if e == nil {
			return nil
		}
		if e.kind != KindList {
			return ErrWrongType
		}
		fn(e.list)
		return nil
	})
}

// LLen returns the length of the list at key.
func (t *Tx) LLen(key string) (int, error) {
	n := 0
	err := t.viewList(key, func(d *deque) { n = d.Len() })
	return n, err
}

// LRange returns elements between start and stop (inclusive, negative
// indexes count from the tail).
func (t *Tx) LRange(key string, start, stop int) ([]string, error) {
	out := []string{}
	err := t.viewList(key, func(d *deque) {
		if s, e, ok := clampRange(start, stop, d.Len()); ok {
			out = d.Slice(s, e)
		}
	})
	return out, err
}

// LIndex returns the element at index.
func (t *Tx) LIndex(key string, index int) (string, bool, error) {
	var (
		v  string
		ok bool
	)
	err := t.viewList(key, func(d *deque) {
		if index < 0 {
			index += d.Len()
		}
		if index >= 0 && index < d.Len() {
			v, ok = d.At(index), true
		}
	})
	return v, ok, err
}

// LSet replaces the element at index.
func (t *Tx) LSet(key string, index int, val string) error {
	return t.write(key, func(e *entry) (*entry, error) {
		if e == nil {
			return nil, ErrNoSuchKey
		}
		if e.kind != KindList {
			return nil, ErrWrongType
		}
		i := index
		if i < 0 {
			i += e.list.Len()
		}
		if i < 0 || i >= e.list.Len() {
			return nil, ErrIndexRange
		}
		e.size += int64(len(val) - len(e.list.At(i)))
		e.list.Set(i, val)
		return e, nil
	})
}

// LTrim keeps only elements between start and stop.
func (t *Tx) LTrim(key string, start, stop int) error {
	return t.update(key, func(e *entry) (*entry, error) {
		if e == nil {
			return nil, nil
		}
		if e.kind != KindList {
			return nil, ErrWrongType
		}
		s, en, ok := clampRange(start, stop, e.list.Len())
		if !ok {
			return nil, nil
		}
		kept := e.list.Slice(s, en)
		e.list.Replace(kept)
		e.size = entryOverhead + int64(len(key))
		for _, v := range kept {
			e.size += listItemSize(v)
		}
		return e, nil
	})
}

// LRem removes occurrences of val: count > 0 from head, count < 0 from
// tail, 0 all of them. Returns the number removed.
func (t *Tx) LRem(key string, count int, val string) (int, error) {
	removed := 0
	err := t.update(key, func(e *entry) (*entry, error) {
		if e == nil {
			return nil, nil
		}
		if e.kind != KindList {
			return nil, ErrWrongType
		}
		vals := e.list.Values()
		kept := make([]string, 0, len(vals))
		limit := count
		if limit < 0 {
			limit = -limit
		}
		if count >= 0 {
			for _, v := range vals {
				if v == val && (limit == 0 || removed < limit) {
					removed++
					continue
				}
				kept = append(kept, v)
			}
		} else {
			drop := make([]bool, len(vals))
			for i := len(vals) - 1; i >= 0 && removed < limit; i-- {
				if vals[i] == val {
					drop[i] = true
					removed++
				}
			}
			for i, v := range vals {
				if !drop[i] {
					kept = append(kept, v)
				}
			}
		}
		if removed == 0 {
			return e, nil
		}
		e.size -= int64(removed) * listItemSize(val)
		if len(kept) == 0 {
			return nil, nil
		}
		e.list.Replace(kept)
		return e, nil
	})
	return removed, err
}
