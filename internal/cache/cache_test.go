package cache

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unsafe"
)

func newTestCache(t *testing.T, opts Options) *BoltCache {
	t.Helper()
	c := New(opts)
	t.Cleanup(c.Close)
	return c
}

func must[T any](t *testing.T) func(T, error) T {
	return func(v T, err error) T {
		t.Helper()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		return v
	}
}

func TestStrings(t *testing.T) {
	c := newTestCache(t, Options{})
	if err := c.Set("k", "v", 0); err != nil {
		t.Fatal(err)
	}
	v, ok, err := c.Get("k")
	if err != nil || !ok || v != "v" {
		t.Fatalf("Get = %q %v %v", v, ok, err)
	}
	if _, ok, _ := c.Get("missing"); ok {
		t.Fatal("missing key found")
	}

	res, _ := c.SetWithOptions("k", "x", SetOptions{NX: true}, true)
	if res.Written || !res.HadOld || res.Old != "v" {
		t.Fatalf("SET NX GET = %+v", res)
	}
	res, _ = c.SetWithOptions("new", "x", SetOptions{XX: true}, false)
	if res.Written {
		t.Fatal("SET XX on missing key wrote")
	}

	if n := must[int64](t)(c.IncrBy("n", 5)); n != 5 {
		t.Fatalf("IncrBy = %d", n)
	}
	if n := must[int64](t)(c.IncrBy("n", -7)); n != -2 {
		t.Fatalf("IncrBy = %d", n)
	}
	if _, err := c.IncrBy("k", 1); !errors.Is(err, ErrNotInteger) {
		t.Fatalf("IncrBy on non-int: %v", err)
	}
	_ = c.Set("max", "9223372036854775807", 0)
	if _, err := c.IncrBy("max", 1); !errors.Is(err, ErrOverflow) {
		t.Fatalf("overflow: %v", err)
	}
	if f := must[float64](t)(c.IncrByFloat("f", 1.5)); f != 1.5 {
		t.Fatalf("IncrByFloat = %v", f)
	}
	if n := must[int](t)(c.Append("k", "yz")); n != 3 {
		t.Fatalf("Append = %d", n)
	}
	if s := must[string](t)(c.GetRange("k", 1, -1)); s != "yz" {
		t.Fatalf("GetRange = %q", s)
	}

	_, _ = c.LPush("list", "a")
	if _, _, err := c.Get("list"); !errors.Is(err, ErrWrongType) {
		t.Fatalf("Get on list: %v", err)
	}
}

func TestExpiration(t *testing.T) {
	c := newTestCache(t, Options{})
	_ = c.Set("k", "v", 30*time.Millisecond)
	d, state, _ := c.TTL("k")
	if state != 0 || d <= 0 || d > 30*time.Millisecond {
		t.Fatalf("TTL = %v %d", d, state)
	}
	time.Sleep(40 * time.Millisecond)
	if _, ok, _ := c.Get("k"); ok {
		t.Fatal("expired key returned")
	}
	if _, state, _ := c.TTL("k"); state != TTLNoKey {
		t.Fatalf("TTL state = %d", state)
	}

	_ = c.Set("p", "v", 0)
	if _, state, _ := c.TTL("p"); state != TTLNoExpire {
		t.Fatalf("TTL state = %d", state)
	}
	if ok := must[bool](t)(c.Expire("p", time.Hour, ExpireXX)); ok {
		t.Fatal("EXPIRE XX applied to persistent key")
	}
	if ok := must[bool](t)(c.Expire("p", time.Hour, ExpireNX)); !ok {
		t.Fatal("EXPIRE NX not applied")
	}
	if ok := must[bool](t)(c.Expire("p", time.Minute, ExpireGT)); ok {
		t.Fatal("EXPIRE GT applied with smaller ttl")
	}
	if ok := must[bool](t)(c.Expire("p", time.Minute, ExpireLT)); !ok {
		t.Fatal("EXPIRE LT not applied")
	}
	// INCR keeps the TTL, SET clears it.
	_ = c.Set("ctr", "1", time.Hour)
	_, _ = c.IncrBy("ctr", 1)
	if _, state, _ := c.TTL("ctr"); state != 0 {
		t.Fatal("INCR dropped TTL")
	}
	_ = c.Set("ctr", "1", 0)
	if _, state, _ := c.TTL("ctr"); state != TTLNoExpire {
		t.Fatal("SET kept TTL")
	}
	if ok := must[bool](t)(c.Persist("p")); !ok {
		t.Fatal("PERSIST failed")
	}
	// Expiring in the past deletes.
	_, _ = c.Expire("p", -time.Second, ExpireAlways)
	if n := must[int](t)(c.Exists("p")); n != 0 {
		t.Fatal("key with past expiry exists")
	}

	_ = c.Set("a", "1", 10*time.Millisecond)
	_ = c.Set("b", "1", 10*time.Millisecond)
	time.Sleep(15 * time.Millisecond)
	// "k" from above expired without being read, so it's reclaimed too.
	if n := c.DeleteExpired(); n != 3 {
		t.Fatalf("DeleteExpired = %d", n)
	}
}

func TestLists(t *testing.T) {
	c := newTestCache(t, Options{})
	if n := must[int](t)(c.LPush("l", "a", "b", "c")); n != 3 {
		t.Fatalf("LPush = %d", n)
	}
	_, _ = c.RPush("l", "d")
	if got := must[[]string](t)(c.LRange("l", 0, -1)); !reflect.DeepEqual(got, []string{"c", "b", "a", "d"}) {
		t.Fatalf("LRange = %v", got)
	}
	if got := must[[]string](t)(c.LPop("l", 1)); !reflect.DeepEqual(got, []string{"c"}) {
		t.Fatalf("LPop = %v", got)
	}
	if got := must[[]string](t)(c.RPop("l", 2)); !reflect.DeepEqual(got, []string{"d", "a"}) {
		t.Fatalf("RPop = %v", got)
	}
	_, _ = c.RPush("l", "x", "b", "y", "b")
	if n := must[int](t)(c.LRem("l", -1, "b")); n != 1 {
		t.Fatalf("LRem = %d", n)
	}
	if got := must[[]string](t)(c.LRange("l", 0, -1)); !reflect.DeepEqual(got, []string{"b", "x", "b", "y"}) {
		t.Fatalf("after LRem = %v", got)
	}
	if err := c.LSet("l", -1, "z"); err != nil {
		t.Fatal(err)
	}
	if err := c.LSet("l", 10, "z"); !errors.Is(err, ErrIndexRange) {
		t.Fatalf("LSet out of range: %v", err)
	}
	_ = c.LTrim("l", 1, 2)
	if got := must[[]string](t)(c.LRange("l", 0, -1)); !reflect.DeepEqual(got, []string{"x", "b"}) {
		t.Fatalf("after LTrim = %v", got)
	}
	_, _ = c.LPop("l", 10)
	if n := must[int](t)(c.Exists("l")); n != 0 {
		t.Fatal("empty list not deleted")
	}
	if n := must[int](t)(c.LPushX("l", "a")); n != 0 {
		t.Fatal("LPUSHX created list")
	}

	// Many pushes/pops exercise deque growth and wraparound.
	for i := 0; i < 1000; i++ {
		_, _ = c.LPush("big", fmt.Sprint(i))
		_, _ = c.RPush("big", fmt.Sprint(i))
	}
	for i := 0; i < 900; i++ {
		_, _ = c.RPop("big", 1)
	}
	got := must[[]string](t)(c.LRange("big", 0, 2))
	if !reflect.DeepEqual(got, []string{"999", "998", "997"}) {
		t.Fatalf("big LRange = %v", got)
	}
	if n := must[int](t)(c.LLen("big")); n != 1100 {
		t.Fatalf("big LLen = %d", n)
	}
}

func TestSetsAndHashes(t *testing.T) {
	c := newTestCache(t, Options{})
	if n := must[int](t)(c.SAdd("s", "a", "b", "a")); n != 2 {
		t.Fatalf("SAdd = %d", n)
	}
	_, _ = c.SAdd("s2", "b", "c")
	if got := must[[]string](t)(c.SInter("s", "s2")); !reflect.DeepEqual(got, []string{"b"}) {
		t.Fatalf("SInter = %v", got)
	}
	if got := must[[]string](t)(c.SUnion("s", "s2")); !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Fatalf("SUnion = %v", got)
	}
	if got := must[[]string](t)(c.SDiff("s", "s2")); !reflect.DeepEqual(got, []string{"a"}) {
		t.Fatalf("SDiff = %v", got)
	}
	if n := must[int](t)(c.SRem("s", "a", "b", "zz")); n != 2 {
		t.Fatalf("SRem = %d", n)
	}
	if n := must[int](t)(c.Exists("s")); n != 0 {
		t.Fatal("empty set not deleted")
	}

	if n := must[int](t)(c.HSet("h", "f1", "v1", "f2", "v2")); n != 2 {
		t.Fatalf("HSet = %d", n)
	}
	if n := must[int](t)(c.HSet("h", "f1", "new")); n != 0 {
		t.Fatalf("HSet update = %d", n)
	}
	if got := must[[]string](t)(c.HGetAll("h")); !reflect.DeepEqual(got, []string{"f1", "new", "f2", "v2"}) {
		t.Fatalf("HGetAll = %v", got)
	}
	if n := must[int64](t)(c.HIncrBy("h", "ctr", 3)); n != 3 {
		t.Fatalf("HIncrBy = %d", n)
	}
	if _, err := c.HIncrBy("h", "f1", 1); err == nil {
		t.Fatal("HIncrBy on non-int field succeeded")
	}
	if ok := must[bool](t)(c.HSetNX("h", "f1", "x")); ok {
		t.Fatal("HSETNX overwrote")
	}
	if n := must[int](t)(c.HDel("h", "f1", "f2", "ctr")); n != 3 {
		t.Fatalf("HDel = %d", n)
	}
	if n := must[int](t)(c.Exists("h")); n != 0 {
		t.Fatal("empty hash not deleted")
	}
}

func TestMemoryAccountingReturnsToZero(t *testing.T) {
	c := newTestCache(t, Options{MaxMemory: 1 << 30})
	_ = c.Set("s", "value", 0)
	_ = c.Set("s", "longer value", 0)
	_, _ = c.Append("s", "!!")
	_, _ = c.RPush("l", "a", "bb", "ccc")
	_ = c.LSet("l", 0, "zzzz")
	_, _ = c.LRem("l", 0, "bb")
	_, _ = c.SAdd("set", "a", "b")
	_, _ = c.HSet("h", "f", "v", "g", "w")
	_, _ = c.HSet("h", "f", "vvvvv")
	_, _ = c.HIncrBy("h", "n", 10)
	_, _ = c.Rename("h", "h-renamed-longer", false)
	if c.UsedMemory() <= 0 || c.UsedMemory() != c.gmem.Load() {
		t.Fatalf("memory = %d (global %d)", c.UsedMemory(), c.gmem.Load())
	}
	_, _ = c.Del("s", "l", "set", "h-renamed-longer")
	if m, k := c.UsedMemory(), c.Len(); m != 0 || k != 0 {
		t.Fatalf("after delete memory=%d keys=%d", m, k)
	}
	if c.gmem.Load() != 0 || c.gkeys.Load() != 0 {
		t.Fatalf("global counters %d %d", c.gmem.Load(), c.gkeys.Load())
	}
}

func TestEvictionMaxKeys(t *testing.T) {
	for _, p := range []Policy{PolicyLRU, PolicyLFU, PolicyRandom, PolicyTTL} {
		t.Run(string(p), func(t *testing.T) {
			c := newTestCache(t, Options{MaxKeys: 100, EvictionPolicy: p})
			for i := 0; i < 1000; i++ {
				if err := c.Set(fmt.Sprint("k", i), "v", 0); err != nil {
					t.Fatal(err)
				}
			}
			if n := c.Len(); n > 100 {
				t.Fatalf("keys = %d, want <= 100", n)
			}
			if c.Stats().Evicted < 900 {
				t.Fatalf("evicted = %d", c.Stats().Evicted)
			}
		})
	}
}

func TestEvictionLRUPrefersColdKeys(t *testing.T) {
	c := newTestCache(t, Options{MaxKeys: 200, EvictionPolicy: PolicyLRU})
	for i := 0; i < 200; i++ {
		_ = c.Set(fmt.Sprint("k", i), "v", 0)
	}
	time.Sleep(2 * time.Millisecond)
	// Keep the first 20 keys hot.
	for i := 0; i < 20; i++ {
		_, _, _ = c.Get(fmt.Sprint("k", i))
	}
	for i := 200; i < 300; i++ {
		_ = c.Set(fmt.Sprint("k", i), "v", 0)
	}
	hot := 0
	for i := 0; i < 20; i++ {
		if _, ok, _ := c.Get(fmt.Sprint("k", i)); ok {
			hot++
		}
	}
	// Sampling is approximate, but hot keys should overwhelmingly survive.
	if hot < 17 {
		t.Fatalf("only %d/20 hot keys survived", hot)
	}
}

func TestNoEviction(t *testing.T) {
	c := newTestCache(t, Options{MaxKeys: 2, EvictionPolicy: PolicyNoEviction})
	_ = c.Set("a", "1", 0)
	_ = c.Set("b", "1", 0)
	_ = c.Set("c", "1", 0) // goes over the limit; next writes are refused
	if err := c.Set("d", "1", 0); !errors.Is(err, ErrOOM) {
		t.Fatalf("want OOM, got %v", err)
	}
	// Deletes are still allowed.
	if _, err := c.Del("a", "b"); err != nil {
		t.Fatal(err)
	}
	if err := c.Set("d", "1", 0); err != nil {
		t.Fatalf("write after freeing: %v", err)
	}
}

func TestMemoryEviction(t *testing.T) {
	c := newTestCache(t, Options{MaxMemory: 64 << 10, EvictionPolicy: PolicyLRU})
	val := strings.Repeat("x", 1000)
	for i := 0; i < 1000; i++ {
		_ = c.Set(fmt.Sprint("k", i), val, 0)
	}
	if m := c.UsedMemory(); m > 64<<10 {
		t.Fatalf("used memory %d over limit", m)
	}
}

func TestAtomicIsolation(t *testing.T) {
	c := newTestCache(t, Options{})
	_ = c.Set("a", "0", 0)
	_ = c.Set("b", "0", 0)
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				_ = c.Atomic([]string{"a", "b"}, false, func(tx *Tx) error {
					a, _, _ := tx.Get("a")
					b, _, _ := tx.Get("b")
					if a != b {
						t.Errorf("torn read: a=%s b=%s", a, b)
					}
					_, _ = tx.IncrBy("a", 1)
					_, _ = tx.IncrBy("b", 1)
					return nil
				})
			}
		}()
	}
	wg.Wait()
	a, _, _ := c.Get("a")
	if a != "4000" {
		t.Fatalf("a = %s", a)
	}
	err := c.Atomic([]string{"a"}, false, func(tx *Tx) error {
		_, _, err := tx.Get("not-declared-key-xyz")
		return err
	})
	// The undeclared key may share a shard with "a"; only check the error
	// type when it doesn't.
	if c.shardIndex("not-declared-key-xyz") != c.shardIndex("a") && !errors.Is(err, ErrUndeclaredKey) {
		t.Fatalf("undeclared access: %v", err)
	}
	// Keyspace-wide commands work when all shards are held.
	err = c.Atomic(nil, true, func(tx *Tx) error {
		keys, err := tx.Keys("*")
		if err != nil || len(keys) != 2 {
			return fmt.Errorf("keys=%v err=%v", keys, err)
		}
		return tx.FlushAll()
	})
	if err != nil || c.Len() != 0 {
		t.Fatalf("atomic flush: %v len=%d", err, c.Len())
	}
}

func TestAtomicPanicReleasesLocks(t *testing.T) {
	c := newTestCache(t, Options{})
	func() {
		defer func() { _ = recover() }()
		_ = c.Atomic([]string{"k"}, false, func(*Tx) error { panic("boom") })
	}()
	done := make(chan struct{})
	go func() { _ = c.Set("k", "v", 0); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("lock still held after panic")
	}
}

func TestConcurrentMixedOps(t *testing.T) {
	// Run with -race: hashes/sets/lists mutated and read concurrently, plus
	// snapshots, used to crash 1.x with "concurrent map read and map write".
	c := newTestCache(t, Options{MaxKeys: 500, CleanupInterval: time.Millisecond})
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 2000; i++ {
				k := fmt.Sprint("k", i%50)
				switch i % 7 {
				case 0:
					_, _ = c.HSet("h"+k, fmt.Sprint("f", g), "v")
				case 1:
					_, _ = c.HGetAll("h" + k)
				case 2:
					_, _ = c.SAdd("s"+k, fmt.Sprint(i))
				case 3:
					_, _ = c.SMembers("s" + k)
				case 4:
					_, _ = c.RPush("l"+k, "x")
				case 5:
					_, _ = c.LPop("l"+k, 1)
				case 6:
					_ = c.Set(k, "v", time.Millisecond)
				}
			}
		}(g)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			_, _ = c.Snapshot(&bytes.Buffer{})
			_, _, _ = c.Scan(0, "*", 100, KindNone)
		}
	}()
	wg.Wait()
}

func TestScanCoversAllKeys(t *testing.T) {
	c := newTestCache(t, Options{})
	want := map[string]bool{}
	for i := 0; i < 5000; i++ {
		k := fmt.Sprint("key:", i)
		_ = c.Set(k, "v", 0)
		want[k] = true
	}
	_, _ = c.SAdd("set:1", "a")
	seen := map[string]bool{}
	var cursor uint64
	for {
		next, keys, err := c.Scan(cursor, "key:*", 100, KindNone)
		if err != nil {
			t.Fatal(err)
		}
		for _, k := range keys {
			seen[k] = true
		}
		if next == 0 {
			break
		}
		cursor = next
	}
	if len(seen) != len(want) {
		t.Fatalf("scan saw %d keys, want %d", len(seen), len(want))
	}
	_, keys, _ := c.Scan(0, "*", 100000, KindSet)
	if !reflect.DeepEqual(keys, []string{"set:1"}) {
		t.Fatalf("TYPE filter = %v", keys)
	}
	if got := must[[]string](t)(c.Keys("key:1?")); len(got) != 10 {
		t.Fatalf("KEYS key:1? = %d", len(got))
	}
}

func TestRename(t *testing.T) {
	c := newTestCache(t, Options{})
	_, _ = c.HSet("src", "f", "v")
	_ = c.Set("dst", "old", 0)
	if ok, err := c.Rename("src", "dst", true); ok || err != nil {
		t.Fatalf("RENAMENX onto existing: %v %v", ok, err)
	}
	if _, err := c.Rename("src", "dst", false); err != nil {
		t.Fatal(err)
	}
	if v, _, _ := c.HGet("dst", "f"); v != "v" {
		t.Fatal("renamed value lost")
	}
	if _, err := c.Rename("nope", "x", false); !errors.Is(err, ErrNoSuchKey) {
		t.Fatalf("rename missing: %v", err)
	}
	if c.Len() != 1 {
		t.Fatalf("len = %d", c.Len())
	}
}

func populate(t *testing.T, c *BoltCache) {
	t.Helper()
	_ = c.Set("str", "hello", 0)
	_ = c.Set("bin", "\x00\xff\xfe binary", 0)
	_ = c.Set("ttl", "soon", time.Hour)
	_ = c.Set("\xffkey", "binary key", 0)
	_ = c.Set("empty", "", 0)
	_, _ = c.RPush("list", "a", "b", "c")
	_, _ = c.SAdd("set", "x", "y")
	_, _ = c.HSet("hash", "name", "Ada", "lang", "Go")
}

func assertPopulated(t *testing.T, c *BoltCache) {
	t.Helper()
	if v, _, _ := c.Get("str"); v != "hello" {
		t.Errorf("str = %q", v)
	}
	if v, _, _ := c.Get("bin"); v != "\x00\xff\xfe binary" {
		t.Errorf("bin = %q", v)
	}
	if v, _, _ := c.Get("\xffkey"); v != "binary key" {
		t.Errorf("binary key = %q", v)
	}
	if _, ok, _ := c.Get("empty"); !ok {
		t.Error("empty string lost")
	}
	if d, state, _ := c.TTL("ttl"); state != 0 || d < 59*time.Minute {
		t.Errorf("ttl = %v %d", d, state)
	}
	if v, _ := c.LRange("list", 0, -1); !reflect.DeepEqual(v, []string{"a", "b", "c"}) {
		t.Errorf("list = %v", v)
	}
	if v, _ := c.SMembers("set"); !reflect.DeepEqual(v, []string{"x", "y"}) {
		t.Errorf("set = %v", v)
	}
	if v, _ := c.HGetAll("hash"); !reflect.DeepEqual(v, []string{"lang", "Go", "name", "Ada"}) {
		t.Errorf("hash = %v", v)
	}
}

func TestSnapshotRoundTrip(t *testing.T) {
	for _, compress := range []bool{false, true} {
		t.Run(fmt.Sprint("compress=", compress), func(t *testing.T) {
			src := newTestCache(t, Options{})
			populate(t, src)
			_ = src.Set("gone", "x", time.Millisecond)
			time.Sleep(2 * time.Millisecond)

			src.DeleteExpired()
			path := filepath.Join(t.TempDir(), "snap.json")
			n, err := src.SaveFile(path, compress)
			if err != nil {
				t.Fatal(err)
			}
			if n != 8 {
				t.Fatalf("saved %d keys", n)
			}
			dst := newTestCache(t, Options{})
			if n, err := dst.LoadFile(path); err != nil || n != 8 {
				t.Fatalf("loaded %d keys: %v", n, err)
			}
			assertPopulated(t, dst)
			if dst.UsedMemory() != src.UsedMemory() {
				t.Errorf("memory mismatch: %d vs %d", dst.UsedMemory(), src.UsedMemory())
			}
		})
	}
}

func TestLoadLegacySnapshot(t *testing.T) {
	// Written by BoltCache 1.x: untyped values, Go time for ExpiresAt.
	legacy := `{
	  "str": {"Value": "hello", "ExpiresAt": "0001-01-01T00:00:00Z"},
	  "list": {"Value": ["a", "b"], "ExpiresAt": "0001-01-01T00:00:00Z"},
	  "set": {"Value": {"x": {}, "y": {}}, "ExpiresAt": "0001-01-01T00:00:00Z"},
	  "hash": {"Value": {"name": "Ada"}, "ExpiresAt": "0001-01-01T00:00:00Z"},
	  "num": {"Value": 42, "ExpiresAt": "0001-01-01T00:00:00Z"},
	  "expired": {"Value": "x", "ExpiresAt": "2001-01-01T00:00:00Z"},
	  "future": {"Value": "x", "ExpiresAt": "2999-01-01T00:00:00Z"}
	}`
	path := filepath.Join(t.TempDir(), "legacy.json")
	if err := os.WriteFile(path, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	c := newTestCache(t, Options{})
	n, err := c.LoadFile(path)
	if err != nil || n != 6 {
		t.Fatalf("loaded %d: %v", n, err)
	}
	if v, _, _ := c.Get("str"); v != "hello" {
		t.Errorf("str = %q", v)
	}
	if v, _ := c.LRange("list", 0, -1); !reflect.DeepEqual(v, []string{"a", "b"}) {
		t.Errorf("list = %v", v)
	}
	if k, _ := c.Type("set"); k != KindSet {
		t.Errorf("set type = %v", k)
	}
	if v, _, _ := c.HGet("hash", "name"); v != "Ada" {
		t.Errorf("hash = %q", v)
	}
	if v, _, _ := c.Get("num"); v != "42" {
		t.Errorf("num = %q", v)
	}
	// The bug that motivated this: HSET after a reload used to wipe the hash.
	_, _ = c.HSet("hash", "age", "36")
	if v, _, _ := c.HGet("hash", "name"); v != "Ada" {
		t.Errorf("hash lost field after HSET: %q", v)
	}
}

func TestPersisterBackups(t *testing.T) {
	dir := t.TempDir()
	c := newTestCache(t, Options{})
	p := NewPersister(c, PersistOptions{File: filepath.Join(dir, "data.json"), BackupCount: 2})
	for i := 0; i < 5; i++ {
		_ = c.Set("k", fmt.Sprint(i), 0)
		if wrote, err := p.Save(false); err != nil || !wrote {
			t.Fatalf("save %d: %v %v", i, wrote, err)
		}
		time.Sleep(2 * time.Millisecond)
	}
	if wrote, _ := p.Save(false); wrote {
		t.Fatal("saved without changes")
	}
	ents, _ := os.ReadDir(dir)
	backups := 0
	for _, e := range ents {
		if strings.Contains(e.Name(), backupMarker) {
			backups++
		}
		if strings.Contains(e.Name(), ".tmp-") {
			t.Fatalf("temp file left behind: %s", e.Name())
		}
	}
	if backups != 2 {
		t.Fatalf("backups = %d, want 2", backups)
	}
	c2 := newTestCache(t, Options{})
	if _, err := NewPersister(c2, PersistOptions{File: filepath.Join(dir, "data.json")}).Load(); err != nil {
		t.Fatal(err)
	}
	if v, _, _ := c2.Get("k"); v != "4" {
		t.Fatalf("reloaded k = %q", v)
	}
}

func BenchmarkSetParallel(b *testing.B) {
	c := New(Options{})
	defer c.Close()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			_ = c.Set(fmt.Sprint("key:", i&0xffff), "value", 0)
			i++
		}
	})
}

func BenchmarkGetParallel(b *testing.B) {
	c := New(Options{})
	defer c.Close()
	keys := make([]string, 1<<16)
	for i := range keys {
		keys[i] = fmt.Sprint("key:", i)
		_ = c.Set(keys[i], "value", 0)
	}
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			_, _, _ = c.Get(keys[i&0xffff])
			i++
		}
	})
}

func TestRenameToUndeclaredKeyKeepsSource(t *testing.T) {
	c := newTestCache(t, Options{})
	_ = c.Set("src", "keep", 0)
	dst := ""
	for i := 0; dst == ""; i++ {
		if k := fmt.Sprint("dst", i); c.shardIndex(k) != c.shardIndex("src") {
			dst = k
		}
	}
	err := c.Atomic([]string{"src"}, false, func(tx *Tx) error {
		_, err := tx.Rename("src", dst, false)
		return err
	})
	if !errors.Is(err, ErrUndeclaredKey) {
		t.Fatalf("err = %v", err)
	}
	if v, ok, _ := c.Get("src"); !ok || v != "keep" {
		t.Fatalf("source lost: %q %v", v, ok)
	}
}

// One entry per key makes its size the main fixed memory cost; keep it in
// the 48-byte size class.
func TestEntrySize(t *testing.T) {
	if n := unsafe.Sizeof(entry{}); n > 48 {
		t.Fatalf("entry is %d bytes, want <= 48", n)
	}
}

// Overwritten values must not accumulate: the table compacts dead records.
func TestOverwriteReleasesOldValue(t *testing.T) {
	c := newTestCache(t, Options{})
	for i := 0; i < 10000; i++ {
		if err := c.Set("k", strings.Repeat(strconv.Itoa(i%10), 64), 0); err != nil {
			t.Fatal(err)
		}
	}
	s := &c.shards[c.shardIndex("k")]
	if n := s.t.live + s.t.dead; n > 2*minChunk {
		t.Fatalf("one key holds %d bytes of records", n)
	}
	if v, _, _ := c.Get("k"); v != strings.Repeat("9", 64) {
		t.Fatalf("value %q", v)
	}
}

func TestMSetAtomicOverwritesAndAccounting(t *testing.T) {
	c := newTestCache(t, Options{})
	if _, err := c.LPush("l", "x"); err != nil {
		t.Fatal(err)
	}
	if err := c.Set("ttl", "v", time.Hour); err != nil {
		t.Fatal(err)
	}
	keys := []string{"l", "ttl", "new"}
	err := c.Atomic(keys, false, func(tx *Tx) error {
		return tx.MSet([]string{"l", "1", "ttl", "2", "new", "3"})
	})
	if err != nil {
		t.Fatal(err)
	}
	for i, k := range keys {
		v, ok, err := c.Get(k)
		if err != nil || !ok || v != strconv.Itoa(i+1) {
			t.Fatalf("%s = %q %v %v", k, v, ok, err)
		}
	}
	if d, state, _ := c.TTL("ttl"); state != TTLNoExpire {
		t.Fatalf("MSET kept a TTL: %v (state %d)", d, state)
	}
	// Undeclared keys are rejected before anything is written.
	err = c.Atomic([]string{"l"}, false, func(tx *Tx) error {
		return tx.MSet([]string{"l", "9", "other-shard-key", "9"})
	})
	if v, _, _ := c.Get("l"); err == nil && c.ShardOf("other-shard-key") != c.ShardOf("l") || v != "1" {
		t.Fatalf("undeclared key: err=%v l=%q", err, v)
	}
	for _, k := range keys {
		if _, err := c.Delete(k); err != nil {
			t.Fatal(err)
		}
	}
	if c.Len() != 0 || c.UsedMemory() != 0 {
		t.Fatalf("after delete: %d keys, %d bytes", c.Len(), c.UsedMemory())
	}
}
