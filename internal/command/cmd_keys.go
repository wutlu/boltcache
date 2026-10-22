package command

import (
	"fmt"
	"strconv"
	"time"

	"github.com/wutlu/boltcache/v2/internal/cache"
	"github.com/wutlu/boltcache/v2/internal/resp"
)

func registerKeyCommands() {
	register("del", cmdDel, -2, flagWrite, 1, -1, 1)
	register("unlink", cmdDel, -2, flagWrite|flagFast, 1, -1, 1)
	register("exists", cmdExists, -2, flagReadOnly|flagFast, 1, -1, 1)
	register("touch", cmdExists, -2, flagReadOnly|flagFast, 1, -1, 1)
	register("type", cmdType, 2, flagReadOnly|flagFast, 1, 1, 1)
	register("expire", cmdExpire(time.Second, false), -3, flagWrite|flagFast, 1, 1, 1)
	register("pexpire", cmdExpire(time.Millisecond, false), -3, flagWrite|flagFast, 1, 1, 1)
	register("expireat", cmdExpire(time.Second, true), -3, flagWrite|flagFast, 1, 1, 1)
	register("pexpireat", cmdExpire(time.Millisecond, true), -3, flagWrite|flagFast, 1, 1, 1)
	register("ttl", cmdTTL(time.Second), 2, flagReadOnly|flagFast, 1, 1, 1)
	register("pttl", cmdTTL(time.Millisecond), 2, flagReadOnly|flagFast, 1, 1, 1)
	register("expiretime", cmdExpireTime(time.Second), 2, flagReadOnly|flagFast, 1, 1, 1)
	register("pexpiretime", cmdExpireTime(time.Millisecond), 2, flagReadOnly|flagFast, 1, 1, 1)
	register("persist", cmdPersist, 2, flagWrite|flagFast, 1, 1, 1)
	register("keys", cmdKeys, 2, flagReadOnly|flagGlobal, 0, 0, 0)
	register("scan", cmdScan, -2, flagReadOnly|flagGlobal, 0, 0, 0)
	register("randomkey", cmdRandomKey, 1, flagReadOnly|flagGlobal, 0, 0, 0)
	register("rename", cmdRename(false), 3, flagWrite, 1, 2, 1)
	register("renamenx", cmdRename(true), 3, flagWrite, 1, 2, 1)
	register("dbsize", cmdDBSize, 1, flagReadOnly|flagFast, 0, 0, 0)
	register("flushall", cmdFlushAll, -1, flagWrite|flagGlobal, 0, 0, 0)
	register("flushdb", cmdFlushAll, -1, flagWrite|flagGlobal, 0, 0, 0)
}

func cmdDel(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
	n, err := db.Del(args[1:]...)
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Int(int64(n))
}

func cmdExists(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
	n, err := db.Exists(args[1:]...)
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Int(int64(n))
}

func cmdType(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
	k, err := db.Type(args[1])
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Simple(k.String())
}

func parseExpireCond(args []string) (cache.ExpireCond, bool) {
	cond := cache.ExpireAlways
	for _, a := range args {
		var c cache.ExpireCond
		switch upper(a) {
		case "NX":
			c = cache.ExpireNX
		case "XX":
			c = cache.ExpireXX
		case "GT":
			c = cache.ExpireGT
		case "LT":
			c = cache.ExpireLT
		default:
			return 0, false
		}
		if cond != cache.ExpireAlways && cond != c {
			return 0, false
		}
		cond = c
	}
	return cond, true
}

func cmdExpire(unit time.Duration, absolute bool) handler {
	return func(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
		n, ok := parseInt(args[2])
		if !ok {
			w.Error(errNotInt)
			return
		}
		cond, ok := parseExpireCond(args[3:])
		if !ok {
			w.Error("ERR NX and XX, GT or LT options at the same time are not compatible")
			return
		}
		if n > int64(1<<62)/int64(unit) || n < -int64(1<<62)/int64(unit) {
			w.Error(fmt.Sprintf(errInvalidTime, lowerASCII(args[0])))
			return
		}
		var at time.Time
		if absolute {
			at = time.Unix(0, 0).Add(time.Duration(n) * unit)
		} else {
			at = time.Now().Add(time.Duration(n) * unit)
		}
		set, err := db.ExpireAt(args[1], at, cond)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeBool(w, set)
	}
}

func cmdTTL(unit time.Duration) handler {
	return func(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
		d, state, err := db.TTL(args[1])
		if err != nil {
			writeErr(w, err)
			return
		}
		if state != 0 {
			w.Int(int64(state))
			return
		}
		// Round up like Redis so a fresh "EXPIRE k 10" reports 10, not 9.
		w.Int(int64((d + unit - 1) / unit))
	}
}

func cmdExpireTime(unit time.Duration) handler {
	return func(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
		at, state, err := db.ExpireTime(args[1])
		if err != nil {
			writeErr(w, err)
			return
		}
		if state != 0 {
			w.Int(int64(state))
			return
		}
		w.Int(at.UnixNano() / int64(unit))
	}
}

func cmdPersist(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
	ok, err := db.Persist(args[1])
	if err != nil {
		writeErr(w, err)
		return
	}
	writeBool(w, ok)
}

func cmdKeys(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
	keys, err := db.Keys(args[1])
	if err != nil {
		writeErr(w, err)
		return
	}
	writeStrings(w, keys)
}

// scanOptions parses MATCH/COUNT/TYPE for SCAN-like commands.
func scanOptions(args []string, allowType bool) (pattern string, count int, kind cache.Kind, err string) {
	count = 10
	for i := 0; i < len(args); i++ {
		opt := upper(args[i])
		if i+1 >= len(args) {
			return "", 0, 0, errSyntax
		}
		val := args[i+1]
		i++
		switch {
		case opt == "MATCH":
			pattern = val
		case opt == "COUNT":
			n, ok := parseInt(val)
			if !ok || n < 1 {
				return "", 0, 0, errSyntax
			}
			count = int(min(n, 1<<20))
		case opt == "TYPE" && allowType:
			switch v := lowerASCII(val); v {
			case "string":
				kind = cache.KindString
			case "list":
				kind = cache.KindList
			case "set":
				kind = cache.KindSet
			case "hash":
				kind = cache.KindHash
			case "zset":
				kind = cache.KindZSet
			default:
				kind = 255 // unknown type: matches nothing
			}
		default:
			return "", 0, 0, errSyntax
		}
	}
	return pattern, count, kind, ""
}

func lowerASCII(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 'a' - 'A'
		}
	}
	return string(b)
}

func cmdScan(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
	cursor, err := strconv.ParseUint(args[1], 10, 64)
	if err != nil {
		w.Error("ERR invalid cursor")
		return
	}
	pattern, count, kind, perr := scanOptions(args[2:], true)
	if perr != "" {
		w.Error(perr)
		return
	}
	var (
		keys []string
		next uint64 // an unknown TYPE matches nothing: iteration ends at 0
	)
	if kind != 255 {
		var serr error
		next, keys, serr = db.Scan(cursor, pattern, count, kind)
		if serr != nil {
			writeErr(w, serr)
			return
		}
	}
	w.Array(2)
	w.Bulk(strconv.FormatUint(next, 10))
	writeStrings(w, keys)
}

func cmdRandomKey(_ *Engine, _ *Session, db *cache.Tx, _ []string, w resp.Writer) {
	k, ok, err := db.RandomKey()
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOptional(w, k, ok)
}

func cmdRename(nx bool) handler {
	return func(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
		ok, err := db.Rename(args[1], args[2], nx)
		if err != nil {
			writeErr(w, err)
			return
		}
		if nx {
			writeBool(w, ok)
		} else {
			w.Simple("OK")
		}
	}
}

func cmdDBSize(_ *Engine, _ *Session, db *cache.Tx, _ []string, w resp.Writer) {
	n, err := db.DBSize()
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Int(n)
}

func cmdFlushAll(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
	if len(args) > 2 || (len(args) == 2 && !eqFold(args[1], "ASYNC") && !eqFold(args[1], "SYNC")) {
		w.Error(errSyntax)
		return
	}
	if err := db.FlushAll(); err != nil {
		writeErr(w, err)
		return
	}
	w.Simple("OK")
}
