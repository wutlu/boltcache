package command

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"

	lua "github.com/yuin/gopher-lua"
	"github.com/yuin/gopher-lua/parse"

	"github.com/wutlu/boltcache/internal/cache"
	"github.com/wutlu/boltcache/internal/resp"
)

// Lua scripting follows Redis semantics:
//
//   - KEYS and ARGV tables, redis.call / redis.pcall / redis.status_reply /
//     redis.error_reply / redis.sha1hex / redis.log.
//   - Redis <-> Lua type conversion rules (nil bulk -> false, integer ->
//     number, status -> {ok=...}, error -> {err=...}; numbers returned to
//     the client are truncated to integers).
//   - Scripts are atomic: the shards of every key passed in KEYS are locked
//     for the duration of the script. Accessing keys that weren't declared in
//     KEYS fails, as does calling keyspace-wide commands such as KEYS.
//   - Global variables don't leak between scripts and a timeout aborts
//     runaway scripts.

func registerScriptCommands() {
	const f = flagScripting | flagNoScript
	register("eval", cmdEval(false, false), -3, f, 0, 0, 0).getKeys = evalKeys
	register("evalsha", cmdEval(true, false), -3, f, 0, 0, 0).getKeys = evalKeys
	register("eval_ro", cmdEval(false, true), -3, f, 0, 0, 0).getKeys = evalKeys
	register("evalsha_ro", cmdEval(true, true), -3, f, 0, 0, 0).getKeys = evalKeys
	register("script", cmdScript, -2, f, 0, 0, 0)
	for _, n := range []string{"eval", "evalsha", "eval_ro", "evalsha_ro"} {
		table[n].flags &^= flagNoKeys
	}
}

func evalKeys(args []string) []string {
	if len(args) < 3 {
		return nil
	}
	n, err := strconv.Atoi(args[2])
	if err != nil || n < 0 || 3+n > len(args) {
		return nil
	}
	return args[3 : 3+n]
}

const maxCachedScripts = 10000

type scriptCache struct {
	mu      sync.RWMutex
	scripts map[string]*lua.FunctionProto
	pool    sync.Pool
}

func newScriptCache() *scriptCache {
	sc := &scriptCache{scripts: map[string]*lua.FunctionProto{}}
	sc.pool.New = func() any { return newVM() }
	return sc
}

func sha1hex(s string) string {
	h := sha1.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}

// load compiles src (or returns the cached compilation) and its SHA1.
func (sc *scriptCache) load(src string) (*lua.FunctionProto, string, error) {
	sha := sha1hex(src)
	sc.mu.RLock()
	proto, ok := sc.scripts[sha]
	sc.mu.RUnlock()
	if ok {
		return proto, sha, nil
	}
	name := "user_script"
	chunk, err := parse.Parse(strings.NewReader(src), name)
	if err != nil {
		return nil, "", fmt.Errorf("ERR Error compiling script (new function): %s", oneLine(err.Error()))
	}
	proto, err = lua.Compile(chunk, name)
	if err != nil {
		return nil, "", fmt.Errorf("ERR Error compiling script (new function): %s", oneLine(err.Error()))
	}
	sc.mu.Lock()
	if len(sc.scripts) >= maxCachedScripts {
		for k := range sc.scripts { // drop an arbitrary entry
			delete(sc.scripts, k)
			break
		}
	}
	sc.scripts[sha] = proto
	sc.mu.Unlock()
	return proto, sha, nil
}

func (sc *scriptCache) get(sha string) (*lua.FunctionProto, bool) {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	p, ok := sc.scripts[strings.ToLower(sha)]
	return p, ok
}

func (sc *scriptCache) flush() {
	sc.mu.Lock()
	sc.scripts = map[string]*lua.FunctionProto{}
	sc.mu.Unlock()
}

func oneLine(s string) string { return strings.ReplaceAll(strings.TrimSpace(s), "\n", " ") }

// vm is a pooled Lua state. cur points at the running script's context so
// the redis.* functions (created once per state) can reach it.
type vm struct {
	L   *lua.LState
	cur *scriptRun
}

type scriptRun struct {
	e        *Engine
	s        *Session
	readOnly bool
}

func newVM() *vm {
	L := lua.NewState(lua.Options{SkipOpenLibs: true, CallStackSize: 256, RegistrySize: 1024 * 20})
	for _, lib := range []struct {
		name string
		fn   lua.LGFunction
	}{
		{lua.BaseLibName, lua.OpenBase},
		{lua.TabLibName, lua.OpenTable},
		{lua.StringLibName, lua.OpenString},
		{lua.MathLibName, lua.OpenMath},
	} {
		L.Push(L.NewFunction(lib.fn))
		L.Push(lua.LString(lib.name))
		L.Call(1, 0)
	}
	// Sandbox: no filesystem access, no dynamic code loading, no output.
	for _, name := range []string{"dofile", "loadfile", "load", "loadstring", "print", "module", "require", "collectgarbage"} {
		L.SetGlobal(name, lua.LNil)
	}
	v := &vm{L: L}
	redis := L.NewTable()
	L.SetFuncs(redis, map[string]lua.LGFunction{
		"call":         func(L *lua.LState) int { return v.call(L, false) },
		"pcall":        func(L *lua.LState) int { return v.call(L, true) },
		"status_reply": luaStatusReply,
		"error_reply":  luaErrorReply,
		"sha1hex":      func(L *lua.LState) int { L.Push(lua.LString(sha1hex(L.CheckString(1)))); return 1 },
		"log":          func(*lua.LState) int { return 0 },
		"setresp":      func(*lua.LState) int { return 0 },
	})
	for i, lvl := range []string{"LOG_DEBUG", "LOG_VERBOSE", "LOG_NOTICE", "LOG_WARNING"} {
		redis.RawSetString(lvl, lua.LNumber(i))
	}
	L.SetGlobal("redis", redis)
	return v
}

func luaStatusReply(L *lua.LState) int {
	t := L.NewTable()
	t.RawSetString("ok", lua.LString(L.CheckString(1)))
	L.Push(t)
	return 1
}

func luaErrorReply(L *lua.LState) int {
	t := L.NewTable()
	t.RawSetString("err", lua.LString(L.CheckString(1)))
	L.Push(t)
	return 1
}

// call implements redis.call/redis.pcall.
func (v *vm) call(L *lua.LState, protected bool) int {
	run := v.cur
	n := L.GetTop()
	if n == 0 {
		return luaFail(L, protected, "ERR Please specify at least one argument for this redis lib call")
	}
	args := make([]string, n)
	for i := 1; i <= n; i++ {
		switch a := L.Get(i).(type) {
		case lua.LString:
			args[i-1] = string(a)
		case lua.LNumber:
			args[i-1] = luaNumberString(a)
		default:
			return luaFail(L, protected, "ERR Lua redis lib command arguments must be strings or integers")
		}
	}
	sp := lookup(args[0])
	switch {
	case sp == nil:
		return luaFail(L, protected, "ERR Unknown Redis command called from script")
	case sp.flags&flagNoScript != 0 || sp.flags&flagNoTx != 0:
		return luaFail(L, protected, "ERR This Redis command is not allowed from script")
	case !sp.arityOK(len(args)):
		return luaFail(L, protected, "ERR Wrong number of args calling Redis command from script")
	case run.readOnly && sp.flags&flagWrite != 0:
		return luaFail(L, protected, "ERR Write commands are not allowed from read-only scripts")
	}
	var rec resp.Recorder
	run.e.run(run.s, sp, args, &rec)
	reply := rec.Result()
	if reply.Kind == resp.KindError {
		return luaFail(L, protected, reply.Str)
	}
	L.Push(toLua(L, reply))
	return 1
}

func luaFail(L *lua.LState, protected bool, msg string) int {
	if protected {
		t := L.NewTable()
		t.RawSetString("err", lua.LString(msg))
		L.Push(t)
		return 1
	}
	L.RaiseError("%s", msg)
	return 0
}

func luaNumberString(n lua.LNumber) string {
	f := float64(n)
	if f == float64(int64(f)) {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'g', 17, 64)
}

// toLua converts a command reply into a Lua value using Redis' rules.
func toLua(L *lua.LState, v resp.Value) lua.LValue {
	switch v.Kind {
	case resp.KindInt:
		return lua.LNumber(v.Int)
	case resp.KindBulk:
		return lua.LString(v.Str)
	case resp.KindSimple:
		t := L.NewTable()
		t.RawSetString("ok", lua.LString(v.Str))
		return t
	case resp.KindError:
		t := L.NewTable()
		t.RawSetString("err", lua.LString(v.Str))
		return t
	case resp.KindArray:
		t := L.CreateTable(len(v.Array), 0)
		for i, e := range v.Array {
			t.RawSetInt(i+1, toLua(L, e))
		}
		return t
	}
	return lua.LFalse // null bulk / null array
}

// writeLua converts a script's return value into a reply.
func writeLua(w resp.Writer, v lua.LValue, depth int) {
	if depth > 64 {
		w.Error("ERR reached lua stack limit")
		return
	}
	switch x := v.(type) {
	case lua.LBool:
		if x {
			w.Int(1)
		} else {
			w.Null()
		}
	case lua.LNumber:
		w.Int(int64(x))
	case lua.LString:
		w.Bulk(string(x))
	case *lua.LTable:
		if ok, isStr := x.RawGetString("ok").(lua.LString); isStr {
			w.Simple(string(ok))
			return
		}
		if msg, isStr := x.RawGetString("err").(lua.LString); isStr {
			w.Error(string(msg))
			return
		}
		n := 0
		for x.RawGetInt(n+1) != lua.LNil {
			n++
		}
		w.Array(n)
		for i := 1; i <= n; i++ {
			writeLua(w, x.RawGetInt(i), depth+1)
		}
	default:
		w.Null()
	}
}

func cmdEval(bySHA, readOnly bool) handler {
	return func(e *Engine, s *Session, _ *cache.Tx, args []string, w resp.Writer) {
		numKeys, err := strconv.Atoi(args[2])
		if err != nil {
			w.Error(errNotInt)
			return
		}
		if numKeys < 0 {
			w.Error("ERR Number of keys can't be negative")
			return
		}
		if 3+numKeys > len(args) {
			w.Error("ERR Number of keys can't be greater than number of args")
			return
		}
		var proto *lua.FunctionProto
		if bySHA {
			p, ok := e.scripts.get(args[1])
			if !ok {
				w.Error("NOSCRIPT No matching script. Please use EVAL.")
				return
			}
			proto = p
		} else {
			p, _, err := e.scripts.load(args[1])
			if err != nil {
				w.Error(err.Error())
				return
			}
			proto = p
		}
		keys, argv := args[3:3+numKeys], args[3+numKeys:]

		if s.tx != nil { // inside MULTI/EXEC: keys are already locked
			e.execScript(s, s.tx, proto, keys, argv, readOnly, w)
			return
		}
		_ = e.cache.Atomic(keys, false, func(tx *cache.Tx) error {
			e.execScript(s, tx, proto, keys, argv, readOnly, w)
			return nil
		})
	}
}

func (e *Engine) execScript(s *Session, tx *cache.Tx, proto *lua.FunctionProto, keys, argv []string, readOnly bool, w resp.Writer) {
	v := e.scripts.pool.Get().(*vm)
	L := v.L

	ss := *s // script-local session sharing auth/identity
	ss.tx, ss.inScript, ss.inMulti = tx, true, false
	v.cur = &scriptRun{e: e, s: &ss, readOnly: readOnly}

	ctx, cancel := context.WithTimeout(context.Background(), e.cfg.ScriptTimeout)
	L.SetContext(ctx)

	env := L.NewTable()
	mt := L.NewTable()
	mt.RawSetString("__index", L.Get(lua.GlobalsIndex))
	L.SetMetatable(env, mt)
	env.RawSetString("KEYS", stringsTable(L, keys))
	env.RawSetString("ARGV", stringsTable(L, argv))
	fn := L.NewFunctionFromProto(proto)
	fn.Env = env

	L.Push(fn)
	err := L.PCall(0, 1, nil)
	if err == nil {
		writeLua(w, L.Get(-1), 0)
	}
	L.SetTop(0)
	L.RemoveContext()
	cancel()
	v.cur = nil

	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			w.Error(fmt.Sprintf("ERR script timed out after %s (features.lua_timeout)", e.cfg.ScriptTimeout))
			// A state interrupted mid-execution may be inconsistent; drop it.
			L.Close()
			return
		}
		w.Error(scriptError(err))
	}
	e.scripts.pool.Put(v)
}

// scriptError extracts the message of a Lua error so errors raised by
// redis.call (which already carry an error code) are passed through.
func scriptError(err error) string {
	var apiErr *lua.ApiError
	msg := err.Error()
	if errors.As(err, &apiErr) && apiErr.Object != nil {
		msg = apiErr.Object.String()
	}
	msg = oneLine(msg)
	// gopher-lua prefixes "user_script:<line>: "; keep it for context but
	// surface Redis error codes from redis.call first.
	if i := strings.Index(msg, ": "); i >= 0 && strings.HasPrefix(msg, "user_script:") {
		rest := msg[i+2:]
		if hasErrorCode(rest) {
			return rest
		}
	}
	if hasErrorCode(msg) {
		return msg
	}
	return "ERR Error running script: " + msg
}

func hasErrorCode(msg string) bool {
	i := 0
	for i < len(msg) && msg[i] >= 'A' && msg[i] <= 'Z' {
		i++
	}
	return i >= 2 && i < len(msg) && msg[i] == ' '
}

func stringsTable(L *lua.LState, vals []string) *lua.LTable {
	t := L.CreateTable(len(vals), 0)
	for i, v := range vals {
		t.RawSetInt(i+1, lua.LString(v))
	}
	return t
}

func cmdScript(e *Engine, _ *Session, _ *cache.Tx, args []string, w resp.Writer) {
	switch upper(args[1]) {
	case "LOAD":
		if len(args) != 3 {
			w.Error(errSyntax)
			return
		}
		_, sha, err := e.scripts.load(args[2])
		if err != nil {
			w.Error(err.Error())
			return
		}
		w.Bulk(sha)
	case "EXISTS":
		w.Array(len(args) - 2)
		for _, sha := range args[2:] {
			_, ok := e.scripts.get(sha)
			writeBool(w, ok)
		}
	case "FLUSH":
		e.scripts.flush()
		w.Simple("OK")
	case "KILL":
		w.Error("NOTBUSY No scripts in execution right now.")
	default:
		w.Error("ERR unknown subcommand '" + args[1] + "'. Try SCRIPT LOAD, EXISTS, FLUSH.")
	}
}
