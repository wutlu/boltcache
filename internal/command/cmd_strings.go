package command

import (
	"fmt"
	"time"

	"github.com/wutlu/boltcache/internal/cache"
	"github.com/wutlu/boltcache/internal/resp"
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
	v, ok, err := db.Get(args[1])
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOptional(w, v, ok)
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
			if n <= 0 || n > 1<<45 {
				return opts, false, fmt.Sprintf(errInvalidTime, "set")
			}
			expiry = true
			switch opt {
			case "EX":
				opts.TTL = time.Duration(n) * time.Second
			case "PX":
				opts.TTL = time.Duration(n) * time.Millisecond
			case "EXAT":
				opts.ExpireAt = time.Unix(n, 0)
			case "PXAT":
				opts.ExpireAt = time.UnixMilli(n)
			}
		default:
			return opts, false, errSyntax
		}
	}
	return opts, get, ""
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
		if n <= 0 || n > 1<<45 {
			w.Error(fmt.Sprintf(errInvalidTime, lowerASCII(args[0])))
			return
		}
		if err := db.Set(args[1], args[3], time.Duration(n)*unit); err != nil {
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
		if n <= 0 || n > 1<<45 {
			w.Error(fmt.Sprintf(errInvalidTime, "getex"))
			return
		}
		switch opt {
		case "EX":
			exp = time.Now().Add(time.Duration(n) * time.Second).UnixNano()
		case "PX":
			exp = time.Now().Add(time.Duration(n) * time.Millisecond).UnixNano()
		case "EXAT":
			exp = time.Unix(n, 0).UnixNano()
		case "PXAT":
			exp = time.UnixMilli(n).UnixNano()
		default:
			w.Error(errSyntax)
			return
		}
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
	for i := 1; i < len(args); i += 2 {
		if err := db.Set(args[i], args[i+1], 0); err != nil {
			writeErr(w, err)
			return
		}
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
