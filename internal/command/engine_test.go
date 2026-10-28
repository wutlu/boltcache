package command

import (
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wutlu/boltcache/v2/internal/cache"
	"github.com/wutlu/boltcache/v2/internal/pubsub"
	"github.com/wutlu/boltcache/v2/internal/resp"
)

type harness struct {
	t testing.TB
	e *Engine
	s *Session
}

func newHarness(t testing.TB, cfg Config) *harness {
	t.Helper()
	c := cache.New(cache.Options{})
	t.Cleanup(c.Close)
	if !cfg.LuaEnabled && !cfg.PubSubEnabled && cfg.Passwords == nil {
		cfg.LuaEnabled, cfg.PubSubEnabled = true, true
	}
	e := New(c, pubsub.NewHub(), cfg)
	return &harness{t: t, e: e, s: e.NewSession("test", nil)}
}

func (h *harness) do(args ...string) resp.Value {
	h.t.Helper()
	return h.doAs(h.s, args...)
}

func (h *harness) doAs(s *Session, args ...string) resp.Value {
	var rec resp.Recorder
	h.e.ExecStrings(s, args, &rec)
	return rec.Result()
}

// render formats a reply compactly for assertions: "OK", "(int) 3",
// "\"bulk\"", "(nil)", "ERR ...", "[a b]".
func render(v resp.Value) string {
	switch v.Kind {
	case resp.KindSimple:
		return v.Str
	case resp.KindError:
		return "!" + v.Str
	case resp.KindInt:
		return fmt.Sprint(v.Int)
	case resp.KindBulk:
		return `"` + v.Str + `"`
	case resp.KindArray:
		parts := make([]string, len(v.Array))
		for i, e := range v.Array {
			parts[i] = render(e)
		}
		return "[" + strings.Join(parts, " ") + "]"
	}
	return "nil"
}

func (h *harness) expect(want string, args ...string) {
	h.t.Helper()
	if got := render(h.do(args...)); got != want {
		h.t.Errorf("%v: got %s, want %s", args, got, want)
	}
}

func (h *harness) expectPrefix(prefix string, args ...string) {
	h.t.Helper()
	if got := render(h.do(args...)); !strings.HasPrefix(got, prefix) {
		h.t.Errorf("%v: got %s, want prefix %s", args, got, prefix)
	}
}

func TestStringCommands(t *testing.T) {
	h := newHarness(t, Config{})
	h.expect("PONG", "PING")
	h.expect(`"hi"`, "ping", "hi")
	h.expect("OK", "SET", "k", "v")
	h.expect(`"v"`, "get", "k")
	h.expect("nil", "GET", "nope")
	h.expect("nil", "SET", "k", "x", "NX")
	h.expect(`"v"`, "SET", "k", "x", "GET")
	h.expect("OK", "SET", "k", "v", "XX", "EX", "100")
	h.expect("100", "TTL", "k")
	h.expect("OK", "SET", "k", "v", "KEEPTTL")
	h.expect("100", "TTL", "k")
	h.expect("!ERR syntax error", "SET", "k", "v", "EX", "1", "PX", "1")
	h.expect("!ERR invalid expire time in 'set' command", "SET", "k", "v", "EX", "0")
	h.expect("!ERR syntax error", "SET", "k", "v", "NX", "XX")
	h.expect("1", "SETNX", "n", "1")
	h.expect("0", "SETNX", "n", "1")
	h.expect("11", "INCRBY", "n", "10")
	h.expect("10", "DECR", "n")
	h.expect(`"10.5"`, "INCRBYFLOAT", "n", "0.5")
	h.expect("!ERR value is not an integer or out of range", "INCR", "n")
	h.expect("OK", "MSET", "a", "1", "b", "2")
	h.expect(`["1" "2" nil]`, "MGET", "a", "b", "c")
	h.expect("0", "MSETNX", "a", "x", "zz", "y")
	h.expect("nil", "GET", "zz")
	h.expect("3", "APPEND", "a", "23")
	h.expect(`"23"`, "GETRANGE", "a", "1", "-1")
	h.expect("3", "STRLEN", "a")
	h.expect(`"123"`, "GETDEL", "a")
	h.expect("0", "EXISTS", "a")
	h.expect("OK", "SETEX", "t", "50", "v")
	h.expect(`"v"`, "GETEX", "t", "PERSIST")
	h.expect("-1", "TTL", "t")
	h.expect("!ERR wrong number of arguments for 'get' command", "GET")
	h.expectPrefix("!ERR unknown command 'FOO'", "FOO", "bar")
}

func TestKeyCommands(t *testing.T) {
	h := newHarness(t, Config{})
	h.do("SET", "user:1", "a")
	h.do("SET", "user:2", "b")
	h.do("RPUSH", "list", "x")
	h.expect(`["user:1" "user:2"]`, "KEYS", "user:*")
	h.expect("string", "TYPE", "user:1")
	h.expect("list", "TYPE", "list")
	h.expect("none", "TYPE", "missing")
	h.expect("2", "DEL", "user:1", "user:2", "missing")
	h.expect("-2", "TTL", "missing")
	h.expect("-1", "TTL", "list")
	h.expect("1", "EXPIRE", "list", "100")
	h.expect("0", "EXPIRE", "list", "200", "NX")
	h.expect("1", "EXPIRE", "list", "200", "GT")
	h.expect("200", "TTL", "list")
	h.expect("1", "PERSIST", "list")
	h.expect("1", "PEXPIRE", "list", "50000")
	if v := h.do("PTTL", "list"); v.Int <= 49000 || v.Int > 50000 {
		t.Errorf("PTTL = %d", v.Int)
	}
	h.expect("OK", "RENAME", "list", "list2")
	h.expect("!ERR no such key", "RENAME", "list", "x")
	h.expect("1", "DBSIZE")
	h.expect(`["0" ["list2"]]`, "SCAN", "0", "MATCH", "l*", "COUNT", "1000")
	h.expect(`["0" []]`, "SCAN", "0", "TYPE", "hash", "COUNT", "1000")
	h.expect("OK", "FLUSHALL")
	h.expect("0", "DBSIZE")
}

func TestCollectionCommands(t *testing.T) {
	h := newHarness(t, Config{})
	h.expect("3", "RPUSH", "l", "a", "b", "c")
	h.expect("4", "LPUSH", "l", "z")
	h.expect(`["z" "a" "b" "c"]`, "LRANGE", "l", "0", "-1")
	h.expect(`"z"`, "LPOP", "l")
	h.expect(`["c" "b"]`, "RPOP", "l", "2")
	h.expect(`"a"`, "LINDEX", "l", "0")
	h.expect(`"a"`, "RPOPLPUSH", "l", "l2")
	h.expect("0", "EXISTS", "l")
	h.expect(`"a"`, "LMOVE", "l2", "l3", "LEFT", "RIGHT")
	h.expect("nil", "LPOP", "l2")
	h.expect("!WRONGTYPE Operation against a key holding the wrong kind of value", "GET", "l3")

	h.expect("2", "SADD", "s", "a", "b")
	h.expect("1", "SISMEMBER", "s", "a")
	h.expect(`[1 0]`, "SMISMEMBER", "s", "a", "x")
	h.expect(`["a" "b"]`, "SMEMBERS", "s")
	h.expect("1", "SADD", "s2", "b")
	h.expect(`["b"]`, "SINTER", "s", "s2")
	h.expect("2", "SUNIONSTORE", "dest", "s", "s2")
	h.expect("2", "SCARD", "dest")
	h.expect("1", "SMOVE", "s", "s2", "a")
	h.expect(`["a" "b"]`, "SMEMBERS", "s2")
	h.expect(`["0" ["a"]]`, "SSCAN", "s2", "0", "MATCH", "a*")

	h.expect("2", "HSET", "h", "name", "Ada", "lang", "Go")
	h.expect("OK", "HMSET", "h", "year", "1815")
	h.expect(`"Ada"`, "HGET", "h", "name")
	h.expect(`["Ada" nil]`, "HMGET", "h", "name", "nope")
	h.expect(`["lang" "Go" "name" "Ada" "year" "1815"]`, "HGETALL", "h")
	h.expect("1816", "HINCRBY", "h", "year", "1")
	h.expect("3", "HLEN", "h")
	h.expect("1", "HDEL", "h", "lang")
	h.expect(`["name" "year"]`, "HKEYS", "h")
	h.expect(`["0" ["name" "Ada"]]`, "HSCAN", "h", "0", "MATCH", "n*")
	h.expect("!ERR wrong number of arguments for 'hset' command", "HSET", "h", "a", "b", "c")
}

func TestMultiExec(t *testing.T) {
	h := newHarness(t, Config{})
	h.expect("OK", "MULTI")
	h.expect("QUEUED", "SET", "a", "1")
	h.expect("QUEUED", "INCR", "a")
	h.expect("QUEUED", "KEYS", "*")
	h.expect("QUEUED", "INFO", "keyspace")
	h.expect("QUEUED", "GET", "a")
	v := h.do("EXEC")
	if len(v.Array) != 5 || render(v.Array[1]) != "2" || render(v.Array[2]) != `["a"]` || render(v.Array[4]) != `"2"` {
		t.Fatalf("EXEC = %s", render(v))
	}
	if !strings.Contains(v.Array[3].Str, "db0:keys=1") {
		t.Fatalf("INFO in EXEC = %q", v.Array[3].Str)
	}
	h.expect("!ERR EXEC without MULTI", "EXEC")

	// Errors while queueing abort the transaction.
	h.do("MULTI")
	h.expectPrefix("!ERR unknown command", "NOPE")
	h.expect("QUEUED", "SET", "a", "x")
	h.expectPrefix("!EXECABORT", "EXEC")
	h.expect(`"2"`, "GET", "a")

	// Runtime errors don't abort other commands.
	h.do("MULTI")
	h.do("INCR", "a")
	h.do("LPUSH", "a", "x")
	h.expect(`[3 !WRONGTYPE Operation against a key holding the wrong kind of value]`, "EXEC")

	h.do("MULTI")
	h.do("SET", "a", "discarded")
	h.expect("OK", "DISCARD")
	h.expect(`"3"`, "GET", "a")
}

func TestWatch(t *testing.T) {
	h := newHarness(t, Config{})
	other := h.e.NewSession("other", nil)
	h.do("SET", "balance", "10")
	h.expect("OK", "WATCH", "balance")
	h.doAs(other, "SET", "balance", "20")
	h.do("MULTI")
	h.do("SET", "balance", "0")
	h.expect("nil", "EXEC") // aborted: null array
	h.expect(`"20"`, "GET", "balance")

	h.do("WATCH", "balance")
	h.do("MULTI")
	h.do("INCRBY", "balance", "5")
	h.expect("[25]", "EXEC")
}

func TestMultiExecIsAtomic(t *testing.T) {
	h := newHarness(t, Config{})
	h.do("MSET", "x", "0", "y", "0")
	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s := h.e.NewSession("g", nil)
			for i := 0; i < 200; i++ {
				h.doAs(s, "MULTI")
				h.doAs(s, "INCR", "x")
				h.doAs(s, "INCR", "y")
				v := h.doAs(s, "EXEC")
				if v.Array[0].Int != v.Array[1].Int {
					t.Errorf("torn transaction: %s", render(v))
					return
				}
			}
		}()
	}
	wg.Wait()
	h.expect(`["800" "800"]`, "MGET", "x", "y")
}

func TestScripting(t *testing.T) {
	h := newHarness(t, Config{})
	h.expect("1", "EVAL", "return 1", "0")
	h.expect(`"hello"`, "EVAL", "return ARGV[1]", "0", "hello")
	h.expect("OK", "EVAL", "return redis.call('SET', KEYS[1], ARGV[1])", "1", "k", "v")
	h.expect(`"v"`, "EVAL", "return redis.call('GET', KEYS[1])", "1", "k")
	h.expect("nil", "EVAL", "return redis.call('GET', KEYS[1])", "1", "missing")
	// Like Redis, arrays stop at the first nil.
	h.expect(`[1 2 "three"]`, "EVAL", "return {1, 2, 'three', nil, 5}", "0")
	h.expect("3", "EVAL", "return 3.99", "0")
	h.expect("1", "EVAL", "return redis.call('GET', KEYS[1]) == false", "1", "missing")
	h.expect("custom", "EVAL", "return redis.status_reply('custom')", "0")
	h.expect("!MYERR bad", "EVAL", "return redis.error_reply('MYERR bad')", "0")
	h.expect("!WRONGTYPE Operation against a key holding the wrong kind of value",
		"EVAL", "redis.call('LPUSH', KEYS[1], 'x')", "1", "k")
	h.expect(`"WRONGTYPE Operation against a key holding the wrong kind of value"`,
		"EVAL", "return redis.pcall('LPUSH', KEYS[1], 'x')['err']", "1", "k")
	h.expectPrefix("!ERR Error compiling script", "EVAL", "return (", "0")
	h.expectPrefix("!ERR Error running script", "EVAL", "error('boom')", "0")
	h.expectPrefix("!ERR Error running script", "EVAL", "return dofile('/etc/passwd')", "0")
	h.expect("!ERR This Redis command is not allowed from script", "EVAL", "return redis.call('EVAL', 'return 1', 0)", "0")
	h.expect("!ERR Write commands are not allowed from read-only scripts", "EVAL_RO", "return redis.call('SET', KEYS[1], 'x')", "1", "k")

	// Rate limiter: the classic EVAL use case.
	limiter := `
		local n = redis.call('INCR', KEYS[1])
		if n == 1 then redis.call('EXPIRE', KEYS[1], ARGV[1]) end
		return n`
	h.expect("1", "EVAL", limiter, "1", "rl", "60")
	h.expect("2", "EVAL", limiter, "1", "rl", "60")
	h.expect("60", "TTL", "rl")

	sha := h.do("SCRIPT", "LOAD", "return KEYS[1]").Str
	if len(sha) != 40 {
		t.Fatalf("sha = %q", sha)
	}
	h.expect(`"x"`, "EVALSHA", sha, "1", "x")
	h.expect(`"x"`, "EVALSHA", strings.ToUpper(sha), "1", "x")
	h.expect("[1 0]", "SCRIPT", "EXISTS", sha, "0000")
	h.expect("OK", "SCRIPT", "FLUSH")
	h.expectPrefix("!NOSCRIPT", "EVALSHA", sha, "0")

	// Globals don't leak between runs.
	h.do("EVAL", "leaked = 42", "0")
	h.expect("nil", "EVAL", "return leaked", "0")

	h.expect("!ERR Number of keys can't be greater than number of args", "EVAL", "return 1", "5", "a")
}

func TestScriptUndeclaredKey(t *testing.T) {
	h := newHarness(t, Config{})
	// Find a key that lives in a different shard than "a".
	c := h.e.Cache()
	other := ""
	for i := 0; i < 10000; i++ {
		k := fmt.Sprint("other", i)
		if c.KeyVersion(k) == c.KeyVersion("a") && k != "a" {
			continue
		}
		other = k
		break
	}
	h.expectPrefix("!ERR script tried accessing a key that was not declared",
		"EVAL", "return redis.call('GET', ARGV[1])", "1", "a", other)
	h.expect("!ERR this command is not allowed inside scripts", "EVAL", "return redis.call('KEYS', '*')", "0")
}

func TestScriptsAreAtomic(t *testing.T) {
	h := newHarness(t, Config{})
	h.do("MSET", "x", "0", "y", "0")
	script := `
		local x = redis.call('INCR', KEYS[1])
		local y = redis.call('INCR', KEYS[2])
		return x - y`
	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s := h.e.NewSession("g", nil)
			for i := 0; i < 200; i++ {
				if v := h.doAs(s, "EVAL", script, "2", "x", "y"); v.Int != 0 {
					t.Errorf("non-atomic script: %s", render(v))
					return
				}
			}
		}()
	}
	wg.Wait()
}

func TestScriptTimeout(t *testing.T) {
	h := newHarness(t, Config{LuaEnabled: true, ScriptTimeout: 50 * time.Millisecond})
	start := time.Now()
	h.expectPrefix("!ERR script timed out", "EVAL", "while true do end", "0")
	if time.Since(start) > 2*time.Second {
		t.Fatal("timeout not enforced")
	}
	h.expect("1", "EVAL", "return 1", "0") // engine still usable
}

func TestFeatureFlags(t *testing.T) {
	h := newHarness(t, Config{Passwords: []string{}})
	h.expectPrefix("!ERR Lua scripting is disabled", "EVAL", "return 1", "0")
	h.expectPrefix("!ERR Pub/Sub is disabled", "PUBLISH", "c", "m")
}

func TestAuth(t *testing.T) {
	h := newHarness(t, Config{Passwords: []string{"s3cret"}, LuaEnabled: true})
	h.expect("PONG", "PING")
	h.expect("!NOAUTH Authentication required.", "GET", "k")
	h.expectPrefix("!WRONGPASS", "AUTH", "nope")
	h.expectPrefix("!WRONGPASS", "AUTH", "admin", "s3cret")
	h.expect("OK", "AUTH", "s3cret")
	h.expect("nil", "GET", "k")

	h2 := &harness{t: t, e: h.e, s: h.e.NewSession("x", nil)}
	h2.expectPrefix("!NOPROTO", "HELLO", "3")
	v := h2.do("HELLO", "2", "AUTH", "default", "s3cret", "SETNAME", "app")
	if v.Kind != resp.KindArray || h2.s.Name != "app" || !h2.s.Authenticated {
		t.Fatalf("HELLO = %s", render(v))
	}

	h3 := newHarness(t, Config{})
	h3.expectPrefix("!ERR AUTH <password> called without any password configured", "AUTH", "x")
}

type chanSub struct {
	mu   sync.Mutex
	msgs []string
}

func (c *chanSub) Deliver(channel, pattern, msg string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.msgs = append(c.msgs, pattern+"|"+channel+"|"+msg)
}

func TestPubSub(t *testing.T) {
	h := newHarness(t, Config{})
	sub := &chanSub{}
	s := h.e.NewSession("sub", sub)
	hs := &harness{t: t, e: h.e, s: s}
	hs.expect(`["subscribe" "news" 1]`, "SUBSCRIBE", "news")
	hs.expect(`["psubscribe" "news.*" 2]`, "PSUBSCRIBE", "news.*")
	hs.expectPrefix("!ERR Can't execute 'get'", "GET", "x")
	hs.expect(`["pong" ""]`, "PING")

	h.expect("1", "PUBLISH", "news", "hello")
	h.expect("1", "PUBLISH", "news.tech", "go")
	h.expect("0", "PUBLISH", "other", "x")
	h.expect(`["news"]`, "PUBSUB", "CHANNELS")
	h.expect(`["news" 1 "x" 0]`, "PUBSUB", "NUMSUB", "news", "x")
	h.expect("1", "PUBSUB", "NUMPAT")
	if want := []string{"|news|hello", "news.*|news.tech|go"}; !reflect.DeepEqual(sub.msgs, want) {
		t.Fatalf("messages = %v", sub.msgs)
	}
	hs.expect(`["unsubscribe" "news" 1]`, "UNSUBSCRIBE")
	hs.expect(`["punsubscribe" "news.*" 0]`, "PUNSUBSCRIBE")
	hs.expect("nil", "GET", "x") // back to normal mode

	hs.do("SUBSCRIBE", "a", "b")
	h.e.CloseSession(s)
	h.expect("0", "PUBLISH", "a", "x")

	// Sessions without a subscriber (REST /command, scripts) can't subscribe.
	h.expectPrefix("!ERR this connection type does not support subscriptions", "SUBSCRIBE", "x")
}

func TestServerCommands(t *testing.T) {
	h := newHarness(t, Config{Version: "9.9.9"})
	h.do("SET", "k", "v")
	info := h.do("INFO").Str
	for _, want := range []string{"boltcache_version:9.9.9", "# Keyspace", "db0:keys=1", "redis_version:"} {
		if !strings.Contains(info, want) {
			t.Errorf("INFO missing %q", want)
		}
	}
	if s := h.do("INFO", "memory").Str; strings.Contains(s, "# Server") || !strings.Contains(s, "used_memory:") {
		t.Errorf("INFO memory = %q", s)
	}
	h.expect("OK", "SELECT", "0")
	h.expect("!ERR DB index is out of range", "SELECT", "1")
	h.expect(`["maxmemory" "0"]`, "CONFIG", "GET", "maxmemory")
	h.expect(`["appendonly" "no" "save" ""]`, "CONFIG", "GET", "save", "appendonly")
	h.expect("OK", "CLIENT", "SETNAME", "worker")
	h.expect(`"worker"`, "CLIENT", "GETNAME")
	h.expect(`["k"]`, "COMMAND", "GETKEYS", "GET", "k")
	if n := h.do("COMMAND", "COUNT").Int; n < 100 {
		t.Errorf("COMMAND COUNT = %d", n)
	}
	h.expectPrefix("!ERR persistence is disabled", "SAVE")
	h.expect("OK", "QUIT")
	if !h.s.Close {
		t.Error("QUIT didn't mark session closed")
	}
}

func TestReviewRegressions(t *testing.T) {
	h := newHarness(t, Config{})
	// Huge relative expirations used to overflow into "no TTL" or a tiny TTL.
	h.expect("!ERR invalid expire time in 'set' command", "SET", "a", "v", "EX", "10000000000")
	h.do("SET", "g", "v")
	h.expect("!ERR invalid expire time in 'getex' command", "GETEX", "g", "EX", "18446744074")
	h.expect(`"v"`, "GET", "g")
	h.expect("OK", "SET", "far", "v", "EXAT", "99999999999")
	if v := h.do("TTL", "far"); v.Int <= 0 {
		t.Errorf("EXAT far future TTL = %d", v.Int)
	}
	h.expect("!ERR invalid expire time in 'setex' command", "SETEX", "a", "10000000000", "v")

	// SCAN with a cursor we never handed out ends the iteration.
	h.expect(`["0" []]`, "SCAN", "18446744073709551615")
}

func TestNoEvictionMultiStepCommands(t *testing.T) {
	c := cache.New(cache.Options{MaxMemory: 2000, EvictionPolicy: cache.PolicyNoEviction})
	t.Cleanup(c.Close)
	h := &harness{t: t, e: New(c, nil, Config{LuaEnabled: true})}
	h.s = h.e.NewSession("t", nil)
	h.do("RPUSH", "src", "a", "b", "c")
	h.do("SADD", "set", "m")
	h.do("SET", "filler", strings.Repeat("x", 3000)) // goes over the limit
	h.expectPrefix("!OOM", "SET", "x", "y")
	h.expectPrefix("!OOM", "LMOVE", "src", "dst", "LEFT", "RIGHT")
	h.expect(`["a" "b" "c"]`, "LRANGE", "src", "0", "-1") // nothing lost
	h.expectPrefix("!OOM", "SMOVE", "set", "set2", "m")
	h.expect(`["m"]`, "SMEMBERS", "set")
	h.expectPrefix("!OOM", "EVAL", "return 1", "0")
	h.expect("1", "DEL", "filler") // freeing memory is allowed
	h.expect(`"a"`, "LMOVE", "src", "dst", "LEFT", "RIGHT")
}

func TestScriptSandboxIsolation(t *testing.T) {
	h := newHarness(t, Config{})
	h.do("SET", "k", "v")
	attacks := []string{
		"redis.call = function() return 'hijacked' end",
		"string.upper = function() return 'hijacked' end",
		"_G.tostring = function() return 'hijacked' end",
		"local mt = getmetatable(_G); if mt then mt.__index.tostring = function() return 'hijacked' end end",
		"local smt = getmetatable(''); if smt then smt.__index.upper = function() return 'hijacked' end end",
		"rawset(_G, 'type', function() return 'hijacked' end)",
	}
	for _, a := range attacks {
		h.do("EVAL", a, "0")
	}
	h.expect(`"v"`, "EVAL", "return redis.call('GET', KEYS[1])", "1", "k")
	h.expect(`"ABC"`, "EVAL", "return string.upper('abc')", "0")
	h.expect(`"ABC"`, "EVAL", "return ('abc'):upper()", "0")
	h.expect(`"1"`, "EVAL", "return tostring(1)", "0")
	h.expect(`"number"`, "EVAL", "return type(1)", "0")
	h.expectPrefix("!ERR Error running script", "EVAL", "return string.rep('x', 1e10)", "0")
	h.expect("3", "EVAL", "return #string.rep('x', 3)", "0")
	h.expectPrefix("!ERR Error running script", "EVAL", "return getfenv(0)", "0")
}

func TestSortedSetCommands(t *testing.T) {
	h := newHarness(t, Config{})
	h.expect("3", "ZADD", "lb", "10", "ada", "20", "bob", "15", "cy")
	h.expect("0", "ZADD", "lb", "NX", "99", "ada")
	h.expect("1", "ZADD", "lb", "CH", "XX", "12", "ada", "1", "nobody")
	h.expect(`"12"`, "ZSCORE", "lb", "ada")
	h.expect(`"14.5"`, "ZINCRBY", "lb", "2.5", "ada")
	h.expect(`["14.5" nil]`, "ZMSCORE", "lb", "ada", "zz")
	h.expect("nil", "ZADD", "lb", "GT", "INCR", "-100", "ada") // update refused
	h.expect(`"15.5"`, "ZADD", "lb", "INCR", "1", "ada")
	h.expect("3", "ZCARD", "lb")
	h.expect(`["cy" "ada" "bob"]`, "ZRANGE", "lb", "0", "-1")
	h.expect(`["bob" "20" "ada" "15.5"]`, "ZRANGE", "lb", "0", "1", "REV", "WITHSCORES")
	h.expect(`["cy" "ada"]`, "ZRANGEBYSCORE", "lb", "15", "(20")
	h.expect(`["ada"]`, "ZRANGEBYSCORE", "lb", "-inf", "+inf", "LIMIT", "1", "1")
	h.expect(`["bob" "ada"]`, "ZRANGE", "lb", "+inf", "(15", "BYSCORE", "REV")
	h.expect(`["bob" "ada"]`, "ZREVRANGEBYSCORE", "lb", "+inf", "(15")
	h.expect(`["bob" "ada" "cy"]`, "ZREVRANGE", "lb", "0", "-1")
	h.expect("2", "ZCOUNT", "lb", "(15", "+inf")
	h.expect("1", "ZRANK", "lb", "ada")
	h.expect("0", "ZREVRANK", "lb", "bob")
	h.expect(`[2 "20"]`, "ZRANK", "lb", "bob", "WITHSCORE")
	h.expect("nil", "ZRANK", "lb", "nobody")
	h.expect(`["cy" "15"]`, "ZPOPMIN", "lb")
	h.expect(`["bob" "20"]`, "ZPOPMAX", "lb", "1")
	h.expect(`["0" ["ada" "15.5"]]`, "ZSCAN", "lb", "0", "MATCH", "a*")
	h.expect("zset", "TYPE", "lb")
	h.expect(`["0" ["lb"]]`, "SCAN", "0", "TYPE", "zset", "COUNT", "10000")
	h.expect("1", "ZREM", "lb", "ada", "nobody")
	h.expect("0", "EXISTS", "lb")

	h.do("ZADD", "r", "1", "a", "2", "b", "3", "c", "4", "d")
	h.expect("2", "ZREMRANGEBYRANK", "r", "0", "1")
	h.expect("1", "ZREMRANGEBYSCORE", "r", "(3", "4")
	h.expect(`["c"]`, "ZRANGE", "r", "0", "-1")

	h.expect(`"inf"`, "ZINCRBY", "inf", "+inf", "m")
	h.expect("!ERR resulting score is not a number (NaN)", "ZINCRBY", "inf", "-inf", "m")
	h.expect("!ERR value is not a valid float", "ZADD", "z", "abc", "m")
	h.expect("!ERR value is not a valid float", "ZADD", "z", "nan", "m")
	h.expect("!ERR XX and NX options at the same time are not compatible", "ZADD", "z", "NX", "XX", "1", "m")
	h.expect("!ERR GT, LT, and/or NX options at the same time are not compatible", "ZADD", "z", "NX", "GT", "1", "m")
	h.expect("!ERR INCR option supports a single increment-element pair", "ZADD", "z", "INCR", "1", "a", "2", "b")
	h.expect("!ERR wrong number of arguments for 'zadd' command", "ZADD", "z", "1")
	h.expect("!ERR syntax error", "ZADD", "z", "1", "m", "2")
	h.expect("!ERR min or max is not a float", "ZCOUNT", "r", "x", "1")
	h.expectPrefix("!ERR syntax error, LIMIT", "ZRANGE", "r", "0", "1", "LIMIT", "0", "1")
	h.do("SET", "str", "x")
	h.expectPrefix("!WRONGTYPE", "ZADD", "str", "1", "m")

	// A leaderboard via Lua: atomically add points and return the new rank.
	h.expect("0", "EVAL", `
		redis.call('ZINCRBY', KEYS[1], ARGV[1], ARGV[2])
		return redis.call('ZREVRANK', KEYS[1], ARGV[2])`, "1", "board", "50", "player1")
}

func TestExecPipelineMatchesSequential(t *testing.T) {
	script := [][]string{
		{"SET", "k", "1"}, {"INCR", "k"}, {"INCR", "k"}, {"GET", "k"},
		{"LPUSH", "k", "x"}, // WRONGTYPE inside a batch
		{"ZADD", "z", "1", "a"}, {"ZADD", "z", "2", "b"}, {"ZINCRBY", "z", "5", "a"}, {"ZRANGE", "z", "0", "-1", "WITHSCORES"},
		{"MULTI"}, {"INCR", "k"}, {"GET", "k"}, {"EXEC"},
		{"NOPE"}, {"GET"}, {"MGET", "k", "z"},
		{"EVAL", "return redis.call('INCR', KEYS[1])", "1", "k"},
		{"DEL", "k"}, {"GET", "k"},
		{"QUIT"}, {"SET", "after", "quit"},
	}
	run := func(pipeline bool) []string {
		h := newHarness(t, Config{})
		var rec resp.Recorder
		if pipeline {
			h.e.ExecPipeline(h.s, script, &rec)
		} else {
			for _, c := range script {
				if h.s.Close {
					break
				}
				h.e.ExecStrings(h.s, c, &rec)
			}
		}
		var out []string
		for _, v := range rec.Results() {
			out = append(out, render(v))
		}
		if v, _, _ := h.e.Cache().Get("after"); v != "" {
			t.Errorf("command after QUIT ran (pipeline=%v)", pipeline)
		}
		return out
	}
	seq, pipe := run(false), run(true)
	if !reflect.DeepEqual(seq, pipe) {
		t.Fatalf("pipeline differs:\nseq  %v\npipe %v", seq, pipe)
	}

	// OOM is still enforced inside a batch.
	c := cache.New(cache.Options{MaxKeys: 1, EvictionPolicy: cache.PolicyNoEviction})
	t.Cleanup(c.Close)
	e := New(c, nil, Config{})
	var rec resp.Recorder
	e.ExecPipeline(e.NewSession("t", nil), [][]string{{"SET", "a", "1"}, {"SET", "a", "2"}, {"SET", "a", "3"}}, &rec)
	_ = c.Set("b", "1", 0) // now over the key limit
	rec.Reset()
	e.ExecPipeline(e.NewSession("t", nil), [][]string{{"SET", "a", "x"}, {"SET", "a", "y"}}, &rec)
	for _, v := range rec.Results() {
		if v.Kind != resp.KindError || !strings.HasPrefix(v.Str, "OOM") {
			t.Fatalf("batched write over the limit: %s", render(v))
		}
	}
}

func TestMultiKeyMatchesKeys(t *testing.T) {
	cases := [][]string{
		{"GET", "a"}, {"DEL", "a"}, {"DEL", "a", "b"}, {"MSET", "a", "1"}, {"MSET", "a", "1", "b", "2"},
		{"RENAME", "a", "b"}, {"LMOVE", "a", "b", "LEFT", "RIGHT"}, {"SINTER", "a"}, {"SINTER", "a", "b"},
		{"EVAL", "x", "0"}, {"EVAL", "x", "2", "a", "b"}, {"PING"}, {"HSET", "h", "f", "v"},
	}
	for _, c := range cases {
		sp := lookup(c[0])
		if got, want := sp.multiKey(c), len(sp.keys(c)) > 1; got != want {
			t.Errorf("%v: multiKey=%v, keys=%v", c, got, sp.keys(c))
		}
	}
}
