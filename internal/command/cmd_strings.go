package command

import (
	"fmt"
	"time"

	"github.com/wutlu/boltcache/v2/internal/cache"
	"github.com/wutlu/boltcache/v2/internal/resp"
)

func registerStringCommands() {
	register("get", cmdGet, 2, flagReadOnly|flagFast, 1, 1, 1)
	register("set", cmdSet, -3, flagWrite, 1, 1, 1)
	register("setnx", cmdSetNX, 3, flagWrite|flagFast, 1, 1, 1)
	register("setex", cmdSetEx(time.Second), 4, flagWrite, 1, 1, 1)
	register("psetex", cmdSetEx(time.Millisecond), 4, flagWrite, 1, 1, 1)
	register("getset", cmdGetSet, 3, flagWrite|flagFast, 1, 1, 1)
	register("getdel", cmdGetDel, 2, flagWrite|flagFast, 1, 1, 1)
	register("getex", cmdGetEx, -2, flagWrite|flagFast, 1, 1, 1)
	register("mget", cmdMGet, -2, flagReadOnly|flagFast, 1, -1, 1)
	register("mset", cmdMSet, -3, flagWrite, 1, -1, 2)
	register("msetnx", cmdMSetNX, -3, flagWrite, 1, -1, 2)
	register("incr", cmdIncrBy(1, false), 2, flagWrite|flagFast, 1, 1, 1)
	register("decr", cmdIncrBy(-1, false), 2, flagWrite|flagFast, 1, 1, 1)
	register("incrby", cmdIncrBy(1, true), 3, flagWrite|flagFast, 1, 1, 1)
	register("decrby", cmdIncrBy(-1, true), 3, flagWrite|flagFast, 1, 1, 1)
	register("incrbyfloat", cmdIncrByFloat, 3, flagWrite|flagFast, 1, 1, 1)
	register("append", cmdAppend, 3, flagWrite|flagFast, 1, 1, 1)
	register("strlen", cmdStrLen, 2, flagReadOnly|flagFast, 1, 1, 1)
	register("getrange", cmdGetRange, 4, flagReadOnly, 1, 1, 1)
	register("substr", cmdGetRange, 4, flagReadOnly, 1, 1, 1)
}

func cmdGet(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
	if err := db.GetTo(args[1], w); err != nil {
		writeErr(w, err)
	}
}

// parseSetArgs parses SET options: EX|PX|EXAT|PXAT|KEEPTTL, NX|XX and GET.
func parseSetArgs(args []string) (opts cache.SetOptions, get bool, errMsg string) {
	expiry := false
	for i := 0; i < len(args); i++ {
		switch opt := upper(args[i]); opt {
		case "NX":
			if opts.XX {
				return opts, false, errSyntax
			}
			opts.NX = true
		case "XX":
			if opts.NX {
				return opts, false, errSyntax
			}
			opts.XX = true
		case "GET":
			get = true
		case "KEEPTTL":
			if expiry {
				return opts, false, errSyntax
			}
			opts.KeepTTL, expiry = true, true
		case "EX", "PX", "EXAT", "PXAT":
			if expiry || i+1 >= len(args) {
				return opts, false, errSyntax
			}
			i++
			n, ok := parseInt(args[i])
			if !ok {
				return opts, false, errNotInt
			}
			at, ok := expiryTime(opt, n)
			if !ok {
				return opts, false, fmt.Sprintf(errInvalidTime, "set")
			}
			expiry = true
			opts.ExpireAt = at
		default:
			return opts, false, errSyntax
		}
	}
	return opts, get, ""
}

// expiryTime converts an EX/PX/EXAT/PXAT argument to an absolute time,
// rejecting non-positive values and relative values that would overflow.
func expiryTime(opt string, n int64) (time.Time, bool) {
	if n <= 0 {
		return time.Time{}, false
	}
	const maxNanos = 1 << 62 // ~146 years; keeps now+ttl far from int64 overflow
	switch opt {
	case "EX":
		if n > maxNanos/int64(time.Second) {
			return time.Time{}, false
		}
		return time.Now().Add(time.Duration(n) * time.Second), true
	case "PX":
		if n > maxNanos/int64(time.Millisecond) {
			return time.Time{}, false
		}
		return time.Now().Add(time.Duration(n) * time.Millisecond), true
	case "EXAT":
		return time.Unix(min(n, 1<<40), 0), true
	case "PXAT":
		return time.UnixMilli(min(n, 1<<50)), true
	}
	return time.Time{}, false
}

func cmdSet(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
	opts, get, msg := parseSetArgs(args[3:])
	if msg != "" {
		w.Error(msg)
		return
	}
	res, err := db.SetWithOptions(args[1], args[2], opts, get)
	if err != nil {
		writeErr(w, err)
		return
	}
	switch {
	case get:
		writeOptional(w, res.Old, res.HadOld)
	case res.Written:
		w.Simple("OK")
	default:
		w.Null()
	}
}

func cmdSetNX(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
	res, err := db.SetWithOptions(args[1], args[2], cache.SetOptions{NX: true}, false)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeBool(w, res.Written)
}

func cmdSetEx(unit time.Duration) handler {
	return func(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
		n, ok := parseInt(args[2])
		if !ok {
			w.Error(errNotInt)
			return
		}
		opt := "EX"
		if unit == time.Millisecond {
			opt = "PX"
		}
		at, ok := expiryTime(opt, n)
		if !ok {
			w.Error(fmt.Sprintf(errInvalidTime, lowerASCII(args[0])))
			return
		}
		if _, err := db.SetWithOptions(args[1], args[3], cache.SetOptions{ExpireAt: at}, false); err != nil {
			writeErr(w, err)
			return
		}
		w.Simple("OK")
	}
}

func cmdGetSet(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
	res, err := db.SetWithOptions(args[1], args[2], cache.SetOptions{}, true)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOptional(w, res.Old, res.HadOld)
}

func cmdGetDel(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
	v, ok, err := db.GetDel(args[1])
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOptional(w, v, ok)
}

func cmdGetEx(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
	var (
		exp     int64
		persist bool
		set     bool
	)
	opts := args[2:]
	for i := 0; i < len(opts); i++ {
		opt := upper(opts[i])
		if set {
			w.Error(errSyntax)
			return
		}
		set = true
		if opt == "PERSIST" {
			persist = true
			continue
		}
		if i+1 >= len(opts) {
			w.Error(errSyntax)
			return
		}
		i++
		n, ok := parseInt(opts[i])
		if !ok {
			w.Error(errNotInt)
			return
		}
		switch opt {
		case "EX", "PX", "EXAT", "PXAT":
		default:
			w.Error(errSyntax)
			return
		}
		at, ok := expiryTime(opt, n)
		if !ok {
			w.Error(fmt.Sprintf(errInvalidTime, "getex"))
			return
		}
		exp = cache.UnixNano(at)
	}
	var (
		v   string
		ok  bool
		err error
	)
	if !set {
		v, ok, err = db.Get(args[1])
	} else {
		v, ok, err = db.GetEx(args[1], exp, persist)
	}
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOptional(w, v, ok)
}

func cmdMGet(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
	vals, oks, err := db.MGet(args[1:]...)
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Array(len(vals))
	for i, v := range vals {
		writeOptional(w, v, oks[i])
	}
}

func cmdMSet(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
	if len(args)%2 != 1 {
		w.Error("ERR wrong number of arguments for 'mset' command")
		return
	}
	if err := db.MSet(args[1:]); err != nil {
		writeErr(w, err)
		return
	}
	w.Simple("OK")
}

func cmdMSetNX(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
	if len(args)%2 != 1 {
		w.Error("ERR wrong number of arguments for 'msetnx' command")
		return
	}
	for i := 1; i < len(args); i += 2 {
		n, err := db.Exists(args[i])
		if err != nil {
			writeErr(w, err)
			return
		}
		if n > 0 {
			w.Int(0)
			return
		}
	}
	for i := 1; i < len(args); i += 2 {
		if err := db.Set(args[i], args[i+1], 0); err != nil {
			writeErr(w, err)
			return
		}
	}
	w.Int(1)
}

func cmdIncrBy(sign int64, withArg bool) handler {
	return func(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
		delta := int64(1)
		if withArg {
			n, ok := parseInt(args[2])
			if !ok || (sign < 0 && n == -1<<63) {
				w.Error(errNotInt)
				return
			}
			delta = n
		}
		n, err := db.IncrBy(args[1], sign*delta)
		if err != nil {
			writeErr(w, err)
			return
		}
		w.Int(n)
	}
}

func cmdIncrByFloat(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
	f, ok := parseFloat(args[2])
	if !ok {
		w.Error(errNotFloat)
		return
	}
	v, err := db.IncrByFloat(args[1], f)
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Bulk(formatFloat(v))
}

func cmdAppend(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
	n, err := db.Append(args[1], args[2])
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Int(int64(n))
}

func cmdStrLen(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
	n, err := db.StrLen(args[1])
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Int(int64(n))
}

func cmdGetRange(_ *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
	start, ok1 := parseInt(args[2])
	end, ok2 := parseInt(args[3])
	if !ok1 || !ok2 {
		w.Error(errNotInt)
		return
	}
	v, err := db.GetRange(args[1], int(start), int(end))
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Bulk(v)
}
