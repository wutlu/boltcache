package command

import (
	"math/rand/v2"
	"strconv"

	"github.com/wutlu/boltcache/internal/cache"
	"github.com/wutlu/boltcache/internal/glob"
	"github.com/wutlu/boltcache/internal/resp"
)

func registerListCommands() {
	register("lpush", cmdPush((*cache.Tx).LPush), -3, flagWrite|flagFast, 1, 1, 1)
	register("rpush", cmdPush((*cache.Tx).RPush), -3, flagWrite|flagFast, 1, 1, 1)
	register("lpushx", cmdPush((*cache.Tx).LPushX), -3, flagWrite|flagFast, 1, 1, 1)
	register("rpushx", cmdPush((*cache.Tx).RPushX), -3, flagWrite|flagFast, 1, 1, 1)
	register("lpop", cmdPop((*cache.Tx).LPop), -2, flagWrite|flagFast, 1, 1, 1)
	register("rpop", cmdPop((*cache.Tx).RPop), -2, flagWrite|flagFast, 1, 1, 1)
	register("llen", cmdLLen, 2, flagReadOnly|flagFast, 1, 1, 1)
	register("lrange", cmdLRange, 4, flagReadOnly, 1, 1, 1)
	register("lindex", cmdLIndex, 3, flagReadOnly, 1, 1, 1)
	register("lset", cmdLSet, 4, flagWrite, 1, 1, 1)
	register("ltrim", cmdLTrim, 4, flagWrite, 1, 1, 1)
	register("lrem", cmdLRem, 4, flagWrite, 1, 1, 1)
	register("rpoplpush", cmdRPopLPush, 3, flagWrite, 1, 2, 1)
	register("lmove", cmdLMove, 5, flagWrite, 1, 2, 1)
}

func registerSetCommands() {
	register("sadd", cmdSAdd, -3, flagWrite|flagFast, 1, 1, 1)
	register("srem", cmdSRem, -3, flagWrite|flagFast, 1, 1, 1)
	register("smembers", cmdSMembers, 2, flagReadOnly, 1, 1, 1)
	register("sismember", cmdSIsMember, 3, flagReadOnly|flagFast, 1, 1, 1)
	register("smismember", cmdSMIsMember, -3, flagReadOnly|flagFast, 1, 1, 1)
	register("scard", cmdSCard, 2, flagReadOnly|flagFast, 1, 1, 1)
	register("spop", cmdSPop, -2, flagWrite|flagFast, 1, 1, 1)
	register("srandmember", cmdSRandMember, -2, flagReadOnly, 1, 1, 1)
	register("sinter", cmdSetOp((*cache.Tx).SInter), -2, flagReadOnly, 1, -1, 1)
	register("sunion", cmdSetOp((*cache.Tx).SUnion), -2, flagReadOnly, 1, -1, 1)
	register("sdiff", cmdSetOp((*cache.Tx).SDiff), -2, flagReadOnly, 1, -1, 1)
	register("sinterstore", cmdSetOpStore((*cache.Tx).SInter), -3, flagWrite, 1, -1, 1)
	register("sunionstore", cmdSetOpStore((*cache.Tx).SUnion), -3, flagWrite, 1, -1, 1)
	register("sdiffstore", cmdSetOpStore((*cache.Tx).SDiff), -3, flagWrite, 1, -1, 1)
	register("smove", cmdSMove, 4, flagWrite|flagFast, 1, 2, 1)
	register("sscan", cmdSScan, -3, flagReadOnly, 1, 1, 1)
}

func registerHashCommands() {
	register("hset", cmdHSet(false), -4, flagWrite|flagFast, 1, 1, 1)
	register("hmset", cmdHSet(true), -4, flagWrite|flagFast, 1, 1, 1)
	register("hsetnx", cmdHSetNX, 4, flagWrite|flagFast, 1, 1, 1)
	register("hget", cmdHGet, 3, flagReadOnly|flagFast, 1, 1, 1)
	register("hmget", cmdHMGet, -3, flagReadOnly|flagFast, 1, 1, 1)
	register("hgetall", cmdHGetAll, 2, flagReadOnly, 1, 1, 1)
	register("hkeys", cmdStringsOf((*cache.Tx).HKeys), 2, flagReadOnly, 1, 1, 1)
	register("hvals", cmdStringsOf((*cache.Tx).HVals), 2, flagReadOnly, 1, 1, 1)
	register("hdel", cmdHDel, -3, flagWrite|flagFast, 1, 1, 1)
	register("hexists", cmdHExists, 3, flagReadOnly|flagFast, 1, 1, 1)
	register("hlen", cmdHLen, 2, flagReadOnly|flagFast, 1, 1, 1)
	register("hstrlen", cmdHStrLen, 3, flagReadOnly|flagFast, 1, 1, 1)
	register("hincrby", cmdHIncrBy, 4, flagWrite|flagFast, 1, 1, 1)
	register("hincrbyfloat", cmdHIncrByFloat, 4, flagWrite|flagFast, 1, 1, 1)
	register("hscan", cmdHScan, -3, flagReadOnly, 1, 1, 1)
}

// --- lists ---

func cmdPush(fn func(*cache.Tx, string, ...string) (int, error)) handler {
	return func(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
		n, err := fn(db, args[1], args[2:]...)
		if err != nil {
			writeErr(w, err)
			return
		}
		w.Int(int64(n))
	}
}

func cmdPop(fn func(*cache.Tx, string, int) ([]string, error)) handler {
	return func(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
		if len(args) > 3 {
			w.Error(errSyntax)
			return
		}
		count, withCount := 1, len(args) == 3
		if withCount {
			n, ok := parseInt(args[2])
			if !ok || n < 0 {
				w.Error("ERR value is out of range, must be positive")
				return
			}
			count = int(n)
		}
		vals, err := fn(db, args[1], count)
		if err != nil {
			writeErr(w, err)
			return
		}
		if !withCount {
			if len(vals) == 0 {
				w.Null()
			} else {
				w.Bulk(vals[0])
			}
			return
		}
		if vals == nil {
			w.NullArray()
			return
		}
		writeStrings(w, vals)
	}
}

func cmdLLen(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
	n, err := db.LLen(args[1])
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Int(int64(n))
}

func cmdLRange(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
	start, ok1 := parseInt(args[2])
	stop, ok2 := parseInt(args[3])
	if !ok1 || !ok2 {
		w.Error(errNotInt)
		return
	}
	vals, err := db.LRange(args[1], clampInt(start), clampInt(stop))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeStrings(w, vals)
}

func clampInt(n int64) int {
	const lim = 1 << 40
	if n > lim {
		return lim
	}
	if n < -lim {
		return -lim
	}
	return int(n)
}

func cmdLIndex(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
	i, ok := parseInt(args[2])
	if !ok {
		w.Error(errNotInt)
		return
	}
	v, found, err := db.LIndex(args[1], clampInt(i))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOptional(w, v, found)
}

func cmdLSet(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
	i, ok := parseInt(args[2])
	if !ok {
		w.Error(errNotInt)
		return
	}
	if err := db.LSet(args[1], clampInt(i), args[3]); err != nil {
		writeErr(w, err)
		return
	}
	w.Simple("OK")
}

func cmdLTrim(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
	start, ok1 := parseInt(args[2])
	stop, ok2 := parseInt(args[3])
	if !ok1 || !ok2 {
		w.Error(errNotInt)
		return
	}
	if err := db.LTrim(args[1], clampInt(start), clampInt(stop)); err != nil {
		writeErr(w, err)
		return
	}
	w.Simple("OK")
}

func cmdLRem(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
	count, ok := parseInt(args[2])
	if !ok {
		w.Error(errNotInt)
		return
	}
	n, err := db.LRem(args[1], clampInt(count), args[3])
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Int(int64(n))
}

// lmove pops from src (head when fromLeft) and pushes onto dst. Runs inside
// Atomic (two keys), so the move is atomic.
func lmove(db *cache.Tx, src, dst string, fromLeft, toLeft bool) (string, bool, error) {
	// Check dst's type first so a failed push can't lose the element.
	if k, err := db.Type(dst); err != nil {
		return "", false, err
	} else if k != cache.KindNone && k != cache.KindList {
		return "", false, cache.ErrWrongType
	}
	var (
		vals []string
		err  error
	)
	if fromLeft {
		vals, err = db.LPop(src, 1)
	} else {
		vals, err = db.RPop(src, 1)
	}
	if err != nil || len(vals) == 0 {
		return "", false, err
	}
	if toLeft {
		_, err = db.LPush(dst, vals[0])
	} else {
		_, err = db.RPush(dst, vals[0])
	}
	return vals[0], err == nil, err
}

func cmdRPopLPush(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
	v, ok, err := lmove(db, args[1], args[2], false, true)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOptional(w, v, ok)
}

func cmdLMove(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
	side := func(s string) (bool, bool) {
		switch upper(s) {
		case "LEFT":
			return true, true
		case "RIGHT":
			return false, true
		}
		return false, false
	}
	from, ok1 := side(args[3])
	to, ok2 := side(args[4])
	if !ok1 || !ok2 {
		w.Error(errSyntax)
		return
	}
	v, ok, err := lmove(db, args[1], args[2], from, to)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOptional(w, v, ok)
}

// --- sets ---

func cmdSAdd(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
	n, err := db.SAdd(args[1], args[2:]...)
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Int(int64(n))
}

func cmdSRem(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
	n, err := db.SRem(args[1], args[2:]...)
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Int(int64(n))
}

func cmdSMembers(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
	vals, err := db.SMembers(args[1])
	if err != nil {
		writeErr(w, err)
		return
	}
	writeStrings(w, vals)
}

func cmdSIsMember(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
	ok, err := db.SIsMember(args[1], args[2])
	if err != nil {
		writeErr(w, err)
		return
	}
	writeBool(w, ok)
}

func cmdSMIsMember(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
	oks, err := db.SMIsMember(args[1], args[2:]...)
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Array(len(oks))
	for _, ok := range oks {
		writeBool(w, ok)
	}
}

func cmdSCard(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
	n, err := db.SCard(args[1])
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Int(int64(n))
}

func cmdSPop(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
	if len(args) > 3 {
		w.Error(errSyntax)
		return
	}
	count, withCount := 1, len(args) == 3
	if withCount {
		n, ok := parseInt(args[2])
		if !ok || n < 0 {
			w.Error("ERR value is out of range, must be positive")
			return
		}
		count = int(min(n, 1<<30))
	}
	vals, err := db.SPop(args[1], count)
	if err != nil {
		writeErr(w, err)
		return
	}
	if withCount {
		if vals == nil {
			vals = []string{}
		}
		writeStrings(w, vals)
		return
	}
	if len(vals) == 0 {
		w.Null()
	} else {
		w.Bulk(vals[0])
	}
}

func cmdSRandMember(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
	if len(args) > 3 {
		w.Error(errSyntax)
		return
	}
	members, err := db.SMembers(args[1])
	if err != nil {
		writeErr(w, err)
		return
	}
	if len(args) == 2 {
		if len(members) == 0 {
			w.Null()
		} else {
			w.Bulk(members[rand.IntN(len(members))])
		}
		return
	}
	n, ok := parseInt(args[2])
	if !ok {
		w.Error(errNotInt)
		return
	}
	var out []string
	switch {
	case n >= 0:
		rand.Shuffle(len(members), func(i, j int) { members[i], members[j] = members[j], members[i] })
		out = members[:min(int(min(n, 1<<30)), len(members))]
	case len(members) > 0:
		// Negative count: may repeat members.
		for i := int64(0); i < -n && i < 1<<20; i++ {
			out = append(out, members[rand.IntN(len(members))])
		}
	}
	if out == nil {
		out = []string{}
	}
	writeStrings(w, out)
}

func cmdSetOp(fn func(*cache.Tx, ...string) ([]string, error)) handler {
	return func(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
		vals, err := fn(db, args[1:]...)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeStrings(w, vals)
	}
}

func cmdSetOpStore(fn func(*cache.Tx, ...string) ([]string, error)) handler {
	return func(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
		vals, err := fn(db, args[2:]...)
		if err != nil {
			writeErr(w, err)
			return
		}
		if _, err := db.Delete(args[1]); err != nil {
			writeErr(w, err)
			return
		}
		if len(vals) > 0 {
			if _, err := db.SAdd(args[1], vals...); err != nil {
				writeErr(w, err)
				return
			}
		}
		w.Int(int64(len(vals)))
	}
}

func cmdSMove(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
	src, dst, member := args[1], args[2], args[3]
	if k, err := db.Type(dst); err != nil {
		writeErr(w, err)
		return
	} else if k != cache.KindNone && k != cache.KindSet {
		writeErr(w, cache.ErrWrongType)
		return
	}
	n, err := db.SRem(src, member)
	if err != nil {
		writeErr(w, err)
		return
	}
	if n == 0 {
		w.Int(0)
		return
	}
	if _, err := db.SAdd(dst, member); err != nil {
		writeErr(w, err)
		return
	}
	w.Int(1)
}

// cmdSScan returns all matching members in a single page (cursor 0), which
// is valid SCAN behaviour for small collections and simple for clients.
func cmdSScan(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
	if _, err := strconv.ParseUint(args[2], 10, 64); err != nil {
		w.Error("ERR invalid cursor")
		return
	}
	pattern, _, _, perr := scanOptions(args[3:], false)
	if perr != "" {
		w.Error(perr)
		return
	}
	members, err := db.SMembers(args[1])
	if err != nil {
		writeErr(w, err)
		return
	}
	out := members[:0]
	for _, m := range members {
		if pattern == "" || glob.Match(pattern, m) {
			out = append(out, m)
		}
	}
	w.Array(2)
	w.Bulk("0")
	writeStrings(w, out)
}

// --- hashes ---

func cmdHSet(legacy bool) handler {
	return func(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
		if len(args)%2 != 0 {
			w.Error("ERR wrong number of arguments for '" + lowerASCII(args[0]) + "' command")
			return
		}
		n, err := db.HSet(args[1], args[2:]...)
		if err != nil {
			writeErr(w, err)
			return
		}
		if legacy {
			w.Simple("OK")
		} else {
			w.Int(int64(n))
		}
	}
}

func cmdHSetNX(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
	ok, err := db.HSetNX(args[1], args[2], args[3])
	if err != nil {
		writeErr(w, err)
		return
	}
	writeBool(w, ok)
}

func cmdHGet(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
	v, ok, err := db.HGet(args[1], args[2])
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOptional(w, v, ok)
}

func cmdHMGet(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
	vals, oks, err := db.HMGet(args[1], args[2:]...)
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Array(len(vals))
	for i, v := range vals {
		writeOptional(w, v, oks[i])
	}
}

func cmdHGetAll(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
	pairs, err := db.HGetAll(args[1])
	if err != nil {
		writeErr(w, err)
		return
	}
	writeStrings(w, pairs)
}

func cmdStringsOf(fn func(*cache.Tx, string) ([]string, error)) handler {
	return func(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
		vals, err := fn(db, args[1])
		if err != nil {
			writeErr(w, err)
			return
		}
		writeStrings(w, vals)
	}
}

func cmdHDel(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
	n, err := db.HDel(args[1], args[2:]...)
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Int(int64(n))
}

func cmdHExists(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
	ok, err := db.HExists(args[1], args[2])
	if err != nil {
		writeErr(w, err)
		return
	}
	writeBool(w, ok)
}

func cmdHLen(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
	n, err := db.HLen(args[1])
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Int(int64(n))
}

func cmdHStrLen(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
	n, err := db.HStrLen(args[1], args[2])
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Int(int64(n))
}

func cmdHIncrBy(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
	d, ok := parseInt(args[3])
	if !ok {
		w.Error(errNotInt)
		return
	}
	n, err := db.HIncrBy(args[1], args[2], d)
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Int(n)
}

func cmdHIncrByFloat(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
	d, ok := parseFloat(args[3])
	if !ok {
		w.Error(errNotFloat)
		return
	}
	f, err := db.HIncrByFloat(args[1], args[2], d)
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Bulk(formatFloat(f))
}

func cmdHScan(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
	if _, err := strconv.ParseUint(args[2], 10, 64); err != nil {
		w.Error("ERR invalid cursor")
		return
	}
	pattern, _, _, perr := scanOptions(args[3:], false)
	if perr != "" {
		w.Error(perr)
		return
	}
	pairs, err := db.HGetAll(args[1])
	if err != nil {
		writeErr(w, err)
		return
	}
	out := pairs[:0]
	for i := 0; i+1 < len(pairs); i += 2 {
		if pattern == "" || glob.Match(pattern, pairs[i]) {
			out = append(out, pairs[i], pairs[i+1])
		}
	}
	w.Array(2)
	w.Bulk("0")
	writeStrings(w, out)
}
