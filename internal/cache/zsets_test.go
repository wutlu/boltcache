package cache

import (
	"fmt"
	"math"
	"math/rand/v2"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

// model is a naive sorted set used to check the skiplist.
type model map[string]float64

func (m model) sorted() []ZMember {
	out := make([]ZMember, 0, len(m))
	for k, v := range m {
		out = append(out, ZMember{k, v})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Score < out[j].Score || (out[i].Score == out[j].Score && out[i].Member < out[j].Member)
	})
	return out
}

func TestZSetAgainstModel(t *testing.T) {
	c := newTestCache(t, Options{})
	m := model{}
	r := rand.New(rand.NewPCG(1, 2))
	for i := 0; i < 20000; i++ {
		member := fmt.Sprint("m", r.IntN(300))
		switch r.IntN(10) {
		case 0, 1, 2, 3, 4:
			score := float64(r.IntN(50)) // many ties to exercise member ordering
			if _, err := c.ZAdd("z", ZAddOptions{}, []ZMember{{member, score}}); err != nil {
				t.Fatal(err)
			}
			m[member] = score
		case 5:
			if _, err := c.ZAdd("z", ZAddOptions{Incr: true}, []ZMember{{member, 1.5}}); err != nil {
				t.Fatal(err)
			}
			m[member] += 1.5
		case 6, 7:
			_, _ = c.ZRem("z", member)
			delete(m, member)
		case 8:
			popped, _ := c.ZPop("z", 2, r.IntN(2) == 0)
			for _, p := range popped {
				delete(m, p.Member)
			}
		case 9:
			want := m.sorted()
			got, _ := c.ZRangeByRank("z", 0, -1, false)
			if len(want) == 0 {
				want = []ZMember{}
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("step %d: range mismatch\n got %v\nwant %v", i, got, want)
			}
			for rank, e := range want {
				if r, _, ok, _ := c.ZRank("z", e.Member, false); !ok || r != rank {
					t.Fatalf("rank of %s = %d, want %d", e.Member, r, rank)
				}
				if r, _, _, _ := c.ZRank("z", e.Member, true); r != len(want)-1-rank {
					t.Fatalf("revrank of %s = %d", e.Member, r)
				}
			}
			lo, hi := float64(r.IntN(60)), float64(r.IntN(60))
			var inRange []ZMember
			for _, e := range want {
				if e.Score > lo && e.Score <= hi {
					inRange = append(inRange, e)
				}
			}
			if inRange == nil {
				inRange = []ZMember{}
			}
			got, _ = c.ZRangeByScore("z", ScoreBound{lo, true}, ScoreBound{hi, false}, false, 0, -1)
			if !reflect.DeepEqual(got, inRange) {
				t.Fatalf("score range (%v,%v]: got %v want %v", lo, hi, got, inRange)
			}
			if n, _ := c.ZCount("z", ScoreBound{lo, true}, ScoreBound{hi, false}); n != len(inRange) {
				t.Fatalf("zcount = %d want %d", n, len(inRange))
			}
		}
	}
}

func TestZAddOptions(t *testing.T) {
	c := newTestCache(t, Options{})
	res, _ := c.ZAdd("z", ZAddOptions{}, []ZMember{{"a", 1}, {"b", 2}})
	if res.Added != 2 {
		t.Fatalf("added %d", res.Added)
	}
	res, _ = c.ZAdd("z", ZAddOptions{NX: true}, []ZMember{{"a", 10}, {"c", 3}})
	if s, _, _ := c.ZScore("z", "a"); s != 1 || res.Added != 1 {
		t.Fatalf("NX: a=%v added=%d", s, res.Added)
	}
	res, _ = c.ZAdd("z", ZAddOptions{XX: true}, []ZMember{{"a", 5}, {"d", 4}})
	if _, ok, _ := c.ZScore("z", "d"); ok || res.Changed != 1 {
		t.Fatalf("XX added d or changed=%d", res.Changed)
	}
	_, _ = c.ZAdd("z", ZAddOptions{GT: true}, []ZMember{{"a", 3}})
	if s, _, _ := c.ZScore("z", "a"); s != 5 {
		t.Fatalf("GT lowered score to %v", s)
	}
	_, _ = c.ZAdd("z", ZAddOptions{LT: true}, []ZMember{{"a", 3}})
	if s, _, _ := c.ZScore("z", "a"); s != 3 {
		t.Fatalf("LT didn't lower score: %v", s)
	}
	_, _ = c.ZAdd("z", ZAddOptions{}, []ZMember{{"inf", math.Inf(1)}, {"ninf", math.Inf(-1)}})
	if _, err := c.ZAdd("z", ZAddOptions{Incr: true}, []ZMember{{"inf", math.Inf(-1)}}); err != ErrScoreNaN {
		t.Fatalf("inf + -inf: %v", err)
	}
	got, _ := c.ZRangeByRank("z", 0, 0, false)
	if got[0].Member != "ninf" {
		t.Fatalf("first = %v", got)
	}
	if _, err := c.ZRem("z", "a", "b", "c", "inf", "ninf"); err != nil || c.Len() != 0 || c.UsedMemory() != 0 {
		t.Fatalf("after removing all: len=%d mem=%d err=%v", c.Len(), c.UsedMemory(), err)
	}
}

func TestZSetSnapshot(t *testing.T) {
	src := newTestCache(t, Options{})
	_, _ = src.ZAdd("lb", ZAddOptions{}, []ZMember{{"ada", 10}, {"bob", -2.5}, {"\xffbin", math.Inf(1)}})
	path := filepath.Join(t.TempDir(), "s.json")
	if _, err := src.SaveFile(path, false); err != nil {
		t.Fatal(err)
	}
	dst := newTestCache(t, Options{})
	if _, err := dst.LoadFile(path); err != nil {
		t.Fatal(err)
	}
	got, _ := dst.ZRangeByRank("lb", 0, -1, false)
	want := []ZMember{{"bob", -2.5}, {"ada", 10}, {"\xffbin", math.Inf(1)}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
	if dst.UsedMemory() != src.UsedMemory() {
		t.Fatalf("memory %d vs %d", dst.UsedMemory(), src.UsedMemory())
	}
}

func BenchmarkZAdd(b *testing.B) {
	c := New(Options{})
	defer c.Close()
	for i := 0; i < b.N; i++ {
		_, _ = c.ZAdd("z", ZAddOptions{}, []ZMember{{fmt.Sprint(i % 100000), float64(i % 1000)}})
	}
}
