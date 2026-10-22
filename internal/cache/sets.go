package cache

import "sort"

func newSet(key string) *entry {
	return &entry{kind: KindSet, set: make(map[string]struct{}), size: entryOverhead + int64(len(key))}
}

func setMemberSize(m string) int64 { return setOverhead + int64(len(m)) }

// SAdd adds members and returns how many were newly added.
func (t *Tx) SAdd(key string, members ...string) (int, error) {
	added := 0
	err := t.write(key, func(e *entry) (*entry, error) {
		if e == nil {
			e = newSet(key)
		} else if e.kind != KindSet {
			return nil, ErrWrongType
		}
		for _, m := range members {
			if _, ok := e.set[m]; !ok {
				e.set[m] = struct{}{}
				e.size += setMemberSize(m)
				added++
			}
		}
		return e, nil
	})
	return added, err
}

// SRem removes members and returns how many were removed.
func (t *Tx) SRem(key string, members ...string) (int, error) {
	removed := 0
	err := t.update(key, func(e *entry) (*entry, error) {
		if e == nil {
			return nil, nil
		}
		if e.kind != KindSet {
			return nil, ErrWrongType
		}
		for _, m := range members {
			if _, ok := e.set[m]; ok {
				delete(e.set, m)
				e.size -= setMemberSize(m)
				removed++
			}
		}
		if len(e.set) == 0 {
			return nil, nil
		}
		return e, nil
	})
	return removed, err
}

func (t *Tx) viewSet(key string, fn func(s map[string]struct{})) error {
	return t.view(key, func(e *entry) error {
		if e == nil {
			return nil
		}
		if e.kind != KindSet {
			return ErrWrongType
		}
		fn(e.set)
		return nil
	})
}

// SMembers returns all members, sorted for deterministic output.
func (t *Tx) SMembers(key string) ([]string, error) {
	out := []string{}
	err := t.viewSet(key, func(s map[string]struct{}) {
		out = make([]string, 0, len(s))
		for m := range s {
			out = append(out, m)
		}
	})
	sort.Strings(out)
	return out, err
}

// SIsMember reports whether member is in the set.
func (t *Tx) SIsMember(key, member string) (bool, error) {
	ok := false
	err := t.viewSet(key, func(s map[string]struct{}) { _, ok = s[member] })
	return ok, err
}

// SMIsMember reports membership for each member.
func (t *Tx) SMIsMember(key string, members ...string) ([]bool, error) {
	out := make([]bool, len(members))
	err := t.viewSet(key, func(s map[string]struct{}) {
		for i, m := range members {
			_, out[i] = s[m]
		}
	})
	return out, err
}

// SCard returns the set cardinality.
func (t *Tx) SCard(key string) (int, error) {
	n := 0
	err := t.viewSet(key, func(s map[string]struct{}) { n = len(s) })
	return n, err
}

// SPop removes and returns up to count random members.
func (t *Tx) SPop(key string, count int) ([]string, error) {
	var out []string
	err := t.update(key, func(e *entry) (*entry, error) {
		if e == nil {
			return nil, nil
		}
		if e.kind != KindSet {
			return nil, ErrWrongType
		}
		for m := range e.set {
			if len(out) >= count {
				break
			}
			out = append(out, m)
		}
		for _, m := range out {
			delete(e.set, m)
			e.size -= setMemberSize(m)
		}
		if len(e.set) == 0 {
			return nil, nil
		}
		return e, nil
	})
	return out, err
}

// SInter returns the intersection of the given sets, sorted.
func (t *Tx) SInter(keys ...string) ([]string, error) {
	sets, err := t.collectSets(keys)
	if err != nil {
		return nil, err
	}
	out := []string{}
	if len(sets) == 0 || sets[0] == nil {
		return out, nil
	}
outer:
	for m := range sets[0] {
		for _, s := range sets[1:] {
			if _, ok := s[m]; !ok {
				continue outer
			}
		}
		out = append(out, m)
	}
	sort.Strings(out)
	return out, nil
}

// SUnion returns the union of the given sets, sorted.
func (t *Tx) SUnion(keys ...string) ([]string, error) {
	sets, err := t.collectSets(keys)
	if err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	for _, s := range sets {
		for m := range s {
			seen[m] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for m := range seen {
		out = append(out, m)
	}
	sort.Strings(out)
	return out, nil
}

// SDiff returns members of the first set not present in the others, sorted.
func (t *Tx) SDiff(keys ...string) ([]string, error) {
	sets, err := t.collectSets(keys)
	if err != nil {
		return nil, err
	}
	out := []string{}
	if len(sets) == 0 {
		return out, nil
	}
outer:
	for m := range sets[0] {
		for _, s := range sets[1:] {
			if _, ok := s[m]; ok {
				continue outer
			}
		}
		out = append(out, m)
	}
	sort.Strings(out)
	return out, nil
}

// collectSets copies each set (nil for missing keys).
func (t *Tx) collectSets(keys []string) ([]map[string]struct{}, error) {
	sets := make([]map[string]struct{}, len(keys))
	for i, k := range keys {
		err := t.viewSet(k, func(s map[string]struct{}) {
			cp := make(map[string]struct{}, len(s))
			for m := range s {
				cp[m] = struct{}{}
			}
			sets[i] = cp
		})
		if err != nil {
			return nil, err
		}
	}
	return sets, nil
}
