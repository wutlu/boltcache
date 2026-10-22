package cache

import (
	"errors"
	"math"
)

// zset is a sorted set: a member->score map for O(1) lookups plus a
// skiplist ordered by (score, member) for ranges and ranks.
type zset struct {
	dict map[string]float64
	zsl  *skiplist
}

// ZMember is a sorted set element.
type ZMember struct {
	Member string
	Score  float64
}

func newZSet(key string) *entry {
	return &entry{kind: KindZSet, zset: &zset{dict: map[string]float64{}, zsl: newSkiplist()},
		size: entryOverhead + int64(len(key))}
}

func zsetMemberSize(m string) int64 { return 64 + int64(len(m)) }

var (
	ErrZAddIncrPair = errors.New("ERR INCR option supports a single increment-element pair")
	ErrScoreNaN     = errors.New("ERR resulting score is not a number (NaN)")
)

// ZAddOptions mirrors ZADD's flags.
type ZAddOptions struct {
	NX, XX, GT, LT bool
	Incr           bool
}

// ZAddResult reports what ZAdd did.
type ZAddResult struct {
	Added   int     // new members
	Changed int     // new or updated members (ZADD ... CH)
	Score   float64 // resulting score with Incr
	Applied bool    // with Incr: false when NX/XX/GT/LT prevented the update
}

func (e *entry) zsetPut(member string, score float64) {
	z := e.zset
	if old, ok := z.dict[member]; ok {
		if old == score {
			return
		}
		z.zsl.delete(old, member)
	} else {
		e.size += zsetMemberSize(member)
	}
	z.dict[member] = score
	z.zsl.insert(score, member)
}

func (e *entry) zsetDel(member string) bool {
	z := e.zset
	score, ok := z.dict[member]
	if !ok {
		return false
	}
	delete(z.dict, member)
	z.zsl.delete(score, member)
	e.size -= zsetMemberSize(member)
	return true
}

// ZAdd adds or updates members.
func (t *Tx) ZAdd(key string, opts ZAddOptions, members []ZMember) (ZAddResult, error) {
	var res ZAddResult
	if opts.Incr && len(members) != 1 {
		return res, ErrZAddIncrPair
	}
	err := t.write(key, func(e *entry) (*entry, error) {
		if e == nil {
			if opts.XX {
				return nil, nil
			}
			e = newZSet(key)
		} else if e.kind != KindZSet {
			return nil, ErrWrongType
		}
		for _, m := range members {
			old, exists := e.zset.dict[m.Member]
			if (opts.NX && exists) || (opts.XX && !exists) {
				continue
			}
			score := m.Score
			if opts.Incr && exists {
				score = old + m.Score
				if math.IsNaN(score) {
					return nil, ErrScoreNaN
				}
			}
			if exists && ((opts.GT && score <= old) || (opts.LT && score >= old)) {
				continue
			}
			res.Score, res.Applied = score, true
			switch {
			case !exists:
				res.Added++
				res.Changed++
			case old != score:
				res.Changed++
			}
			e.zsetPut(m.Member, score)
		}
		if len(e.zset.dict) == 0 {
			return nil, nil
		}
		return e, nil
	})
	return res, err
}

func (t *Tx) viewZSet(key string, fn func(z *zset)) error {
	return t.view(key, func(e *entry) error {
		if e == nil {
			return nil
		}
		if e.kind != KindZSet {
			return ErrWrongType
		}
		fn(e.zset)
		return nil
	})
}

// ZScore returns a member's score.
func (t *Tx) ZScore(key, member string) (float64, bool, error) {
	var (
		s  float64
		ok bool
	)
	err := t.viewZSet(key, func(z *zset) { s, ok = z.dict[member] })
	return s, ok, err
}

// ZMScore returns the scores of several members.
func (t *Tx) ZMScore(key string, members ...string) ([]float64, []bool, error) {
	scores := make([]float64, len(members))
	oks := make([]bool, len(members))
	err := t.viewZSet(key, func(z *zset) {
		for i, m := range members {
			scores[i], oks[i] = z.dict[m]
		}
	})
	return scores, oks, err
}

// ZCard returns the number of members.
func (t *Tx) ZCard(key string) (int, error) {
	n := 0
	err := t.viewZSet(key, func(z *zset) { n = len(z.dict) })
	return n, err
}

// ZRank returns a member's 0-based rank (from the highest score when rev).
func (t *Tx) ZRank(key, member string, rev bool) (rank int, score float64, ok bool, err error) {
	err = t.viewZSet(key, func(z *zset) {
		s, found := z.dict[member]
		if !found {
			return
		}
		r := z.zsl.rank(s, member)
		if rev {
			r = z.zsl.length - 1 - r
		}
		rank, score, ok = r, s, true
	})
	return
}

// ZRem removes members and returns how many existed.
func (t *Tx) ZRem(key string, members ...string) (int, error) {
	removed := 0
	err := t.update(key, func(e *entry) (*entry, error) {
		if e == nil {
			return nil, nil
		}
		if e.kind != KindZSet {
			return nil, ErrWrongType
		}
		for _, m := range members {
			if e.zsetDel(m) {
				removed++
			}
		}
		if len(e.zset.dict) == 0 {
			return nil, nil
		}
		return e, nil
	})
	return removed, err
}

// rankRange collects members between ranks start and stop (inclusive,
// negative counts from the end), in reverse order when rev.
func (z *zset) rankRange(start, stop int, rev bool) []ZMember {
	s, e, ok := clampRange(start, stop, z.zsl.length)
	if !ok {
		return []ZMember{}
	}
	out := make([]ZMember, 0, e-s+1)
	if rev {
		x := z.zsl.byRank(z.zsl.length - 1 - s)
		for i := s; i <= e && x != nil; i++ {
			out = append(out, ZMember{x.member, x.score})
			x = x.backward
		}
		return out
	}
	x := z.zsl.byRank(s)
	for i := s; i <= e && x != nil; i++ {
		out = append(out, ZMember{x.member, x.score})
		x = x.level[0].forward
	}
	return out
}

// scoreRange collects members with min <= score <= max, skipping offset and
// returning at most count (count < 0 = all), descending when rev.
func (z *zset) scoreRange(min, max ScoreBound, rev bool, offset, count int) []ZMember {
	out := []ZMember{}
	var x *zNode
	if rev {
		x = z.zsl.lastInRange(min, max)
	} else {
		x = z.zsl.firstInRange(min, max)
	}
	for ; x != nil && offset > 0; offset-- {
		if rev {
			x = x.backward
		} else {
			x = x.level[0].forward
		}
	}
	for x != nil && count != 0 {
		if rev && !min.aboveMin(x.score) || !rev && !max.belowMax(x.score) {
			break
		}
		out = append(out, ZMember{x.member, x.score})
		count--
		if rev {
			x = x.backward
		} else {
			x = x.level[0].forward
		}
	}
	return out
}

// ZRangeByRank returns members by rank.
func (t *Tx) ZRangeByRank(key string, start, stop int, rev bool) ([]ZMember, error) {
	out := []ZMember{}
	err := t.viewZSet(key, func(z *zset) { out = z.rankRange(start, stop, rev) })
	return out, err
}

// ZRangeByScore returns members by score.
func (t *Tx) ZRangeByScore(key string, min, max ScoreBound, rev bool, offset, count int) ([]ZMember, error) {
	out := []ZMember{}
	err := t.viewZSet(key, func(z *zset) { out = z.scoreRange(min, max, rev, offset, count) })
	return out, err
}

// ZCount returns the number of members with a score in [min, max].
func (t *Tx) ZCount(key string, min, max ScoreBound) (int, error) {
	n := 0
	err := t.viewZSet(key, func(z *zset) {
		first := z.zsl.firstInRange(min, max)
		if first == nil {
			return
		}
		last := z.zsl.lastInRange(min, max)
		n = z.zsl.rank(last.score, last.member) - z.zsl.rank(first.score, first.member) + 1
	})
	return n, err
}

// ZPop removes and returns up to count members with the lowest (or highest
// when max) scores.
func (t *Tx) ZPop(key string, count int, max bool) ([]ZMember, error) {
	var out []ZMember
	err := t.update(key, func(e *entry) (*entry, error) {
		if e == nil {
			return nil, nil
		}
		if e.kind != KindZSet {
			return nil, ErrWrongType
		}
		for i := 0; i < count && e.zset.zsl.length > 0; i++ {
			x := e.zset.zsl.header.level[0].forward
			if max {
				x = e.zset.zsl.tail
			}
			out = append(out, ZMember{x.member, x.score})
			e.zsetDel(x.member)
		}
		if len(e.zset.dict) == 0 {
			return nil, nil
		}
		return e, nil
	})
	return out, err
}

func (t *Tx) zremRange(key string, pick func(z *zset) []ZMember) (int, error) {
	removed := 0
	err := t.update(key, func(e *entry) (*entry, error) {
		if e == nil {
			return nil, nil
		}
		if e.kind != KindZSet {
			return nil, ErrWrongType
		}
		for _, m := range pick(e.zset) {
			e.zsetDel(m.Member)
			removed++
		}
		if len(e.zset.dict) == 0 {
			return nil, nil
		}
		return e, nil
	})
	return removed, err
}

// ZRemRangeByRank removes members between ranks start and stop.
func (t *Tx) ZRemRangeByRank(key string, start, stop int) (int, error) {
	return t.zremRange(key, func(z *zset) []ZMember { return z.rankRange(start, stop, false) })
}

// ZRemRangeByScore removes members with a score in [min, max].
func (t *Tx) ZRemRangeByScore(key string, min, max ScoreBound) (int, error) {
	return t.zremRange(key, func(z *zset) []ZMember { return z.scoreRange(min, max, false, 0, -1) })
}

// ZMembers returns every member in order (used by ZSCAN and snapshots).
func (t *Tx) ZMembers(key string) ([]ZMember, error) {
	return t.ZRangeByRank(key, 0, -1, false)
}
