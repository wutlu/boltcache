package cache

import (
	"hash/maphash"
	"math/rand/v2"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"
)

// TestTableRandomized checks a table against a map under random puts,
// overwrites, expirations, collections and deletes, through rehashes and
// compactions.
func TestTableRandomized(t *testing.T) {
	for _, withMeta := range []bool{false, true} {
		seed := maphash.MakeSeed()
		tb := table{seed: seed, withMeta: withMeta}
		h := func(k string) uint64 { return maphash.String(seed, k) }
		ref := map[string]tableWant{}
		r := rand.New(rand.NewPCG(1, 2))
		key := func() string { return "k" + strconv.Itoa(r.IntN(3000)) }
		for step := 0; step < 200000; step++ {
			k := key()
			switch op := r.IntN(10); {
			case op < 4: // set string
				v := strings.Repeat("v", r.IntN(40))
				var exp int64
				if r.IntN(3) == 0 {
					exp = int64(r.IntN(1e9) + 1)
				}
				tb.putString(k, v, exp, h(k))
				ref[k] = tableWant{str: v, expire: exp}
			case op < 5: // collection
				e := &entry{kind: KindHash}
				tb.putColl(k, e, h(k))
				ref[k] = tableWant{coll: e}
			case op < 6: // change expiration of a string
				w, ok := ref[k]
				i := tb.find(k, h(k))
				if ok != (i >= 0) {
					t.Fatalf("find(%s) = %d, present %v", k, i, ok)
				}
				if ok && w.coll == nil {
					exp := int64(r.IntN(3)) * 12345
					tb.setExpire(i, k, h(k), exp)
					w.expire = exp
					ref[k] = w
				}
			default: // delete
				if i := tb.find(k, h(k)); i >= 0 {
					tb.remove(i)
					delete(ref, k)
				}
			}
			if step%5000 == 0 {
				checkTable(t, &tb, ref, h)
			}
		}
		checkTable(t, &tb, ref, h)
		// Deleting during iteration.
		tb.each(func(i int) bool {
			tb.remove(i)
			return true
		})
		if tb.count != 0 {
			t.Fatalf("%d keys left after deleting all", tb.count)
		}
	}
}

type tableWant struct {
	str    string
	expire int64
	coll   *entry
}

func checkTable(t *testing.T, tb *table, ref map[string]tableWant, h func(string) uint64) {
	t.Helper()
	if tb.count != len(ref) {
		t.Fatalf("count %d, want %d", tb.count, len(ref))
	}
	seen := 0
	tb.each(func(i int) bool {
		seen++
		r := tb.decode(tb.slots[i])
		if _, ok := ref[r.key]; !ok {
			t.Fatalf("unexpected key %q", r.key)
		}
		return true
	})
	if seen != len(ref) {
		t.Fatalf("iterated %d keys, want %d", seen, len(ref))
	}
	for k, w := range ref {
		i := tb.find(k, h(k))
		if i < 0 {
			t.Fatalf("key %q missing", k)
		}
		r := tb.decode(tb.slots[i])
		if w.coll != nil {
			if r.kind != KindHash || tb.coll(r) != w.coll {
				t.Fatalf("%q: wrong collection", k)
			}
		} else if r.kind != KindString || r.str != w.str || r.expireAt != w.expire {
			t.Fatalf("%q = (%v %q %d), want (%q %d)", k, r.kind, r.str, r.expireAt, w.str, w.expire)
		}
		if tb.withMeta && unsafePointer(r.meta)%8 != 0 {
			t.Fatalf("%q: meta not aligned", k)
		}
	}
	if tb.live < 0 || tb.dead < 0 {
		t.Fatalf("live %d dead %d", tb.live, tb.dead)
	}
	// Dead space is reclaimed: never much more than live data.
	if tb.dead > tb.live+minChunk {
		t.Fatalf("dead %d bytes vs live %d", tb.dead, tb.live)
	}
}

func unsafePointer[T any](p *T) uintptr { return uintptr(unsafe.Pointer(p)) }

type checkWriter struct {
	t   *testing.T
	bad *atomic.Bool
}

func (w checkWriter) Bulk(s string) {
	if strings.Count(s, s[:1]) != len(s) {
		w.bad.Store(true)
	}
}
func (w checkWriter) Null() {}

// Same-length values are overwritten in place; readers must still never see
// a mix of two values.
func TestInPlaceOverwriteNoTornReads(t *testing.T) {
	c := newTestCache(t, Options{})
	_ = c.Set("k", strings.Repeat("a", 256), 0)
	var bad atomic.Bool
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for w := 0; w < 2; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; ; i++ {
				select {
				case <-stop:
					return
				default:
				}
				_ = c.Set("k", strings.Repeat(string(rune('a'+(i+w)%26)), 256), 0)
			}
		}(w)
	}
	for r := 0; r < 3; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 20000; i++ {
				v, _, _ := c.Get("k")
				if strings.Count(v, v[:1]) != len(v) {
					bad.Store(true)
				}
				_ = c.GetTo("k", checkWriter{t, &bad})
				vals, _, _ := c.MGet("k")
				// A value read earlier must not change afterwards.
				if strings.Count(vals[0], vals[0][:1]) != len(vals[0]) || strings.Count(v, v[:1]) != len(v) {
					bad.Store(true)
				}
			}
		}()
	}
	time.Sleep(200 * time.Millisecond)
	close(stop)
	wg.Wait()
	if bad.Load() {
		t.Fatal("torn read")
	}
}
