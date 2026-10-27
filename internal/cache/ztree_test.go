package cache

import (
	"fmt"
	"math/rand/v2"
	"sort"
	"testing"
)

// check verifies the tree's invariants and returns its items in order.
func (t *ztree) check(tb testing.TB) []zitem {
	tb.Helper()
	var walk func(n *ztNode, depth int) (size int, max zitem, leafDepth int)
	leafDepth := -1
	walk = func(n *ztNode, depth int) (int, zitem, int) {
		if n.leaf() {
			if len(n.items) > ztMax {
				tb.Fatalf("leaf with %d items", len(n.items))
			}
			for i := 1; i < len(n.items); i++ {
				if !n.items[i-1].less(n.items[i]) {
					tb.Fatalf("leaf not sorted at %d", i)
				}
			}
			if leafDepth == -1 {
				leafDepth = depth
			} else if leafDepth != depth {
				tb.Fatalf("leaves at depths %d and %d", leafDepth, depth)
			}
			var m zitem
			if len(n.items) > 0 {
				m = n.items[len(n.items)-1]
			}
			return len(n.items), m, depth
		}
		if len(n.children) == 0 || len(n.children) > ztMax || len(n.counts) != len(n.children) || len(n.maxs) != len(n.children) {
			tb.Fatalf("bad internal node: %d children", len(n.children))
		}
		total := 0
		for i, c := range n.children {
			size, m, _ := walk(c, depth+1)
			if size == 0 {
				tb.Fatal("empty child")
			}
			if size != n.counts[i] {
				tb.Fatalf("count %d != subtree size %d", n.counts[i], size)
			}
			if m != n.maxs[i] {
				tb.Fatalf("max %v != subtree max %v", n.maxs[i], m)
			}
			total += size
		}
		return total, n.maxs[len(n.maxs)-1], depth
	}
	size, _, _ := walk(t.root, 0)
	if size != t.length {
		tb.Fatalf("length %d != size %d", t.length, size)
	}
	// Leaf links visit every item in order, both ways.
	var items []zitem
	for p := t.at(0); p.valid(); p = p.next() {
		items = append(items, p.item())
	}
	if len(items) != t.length {
		tb.Fatalf("forward iteration saw %d of %d", len(items), t.length)
	}
	n := 0
	for p := t.at(t.length - 1); p.valid(); p = p.prev() {
		if p.item() != items[len(items)-1-n] {
			tb.Fatal("backward iteration differs")
		}
		n++
	}
	return items
}

func TestZTreeRandomized(t *testing.T) {
	for seed := uint64(1); seed <= 4; seed++ {
		r := rand.New(rand.NewPCG(seed, seed*7))
		tree := newZTree()
		ref := map[string]float64{}
		target := 20000 + r.IntN(80000)
		ops := 0
		grow := true
		var keys []string // members in ref, for picking deletions while shrinking
		for grow || len(ref) > 10 {
			if len(ref) >= target {
				grow = false
				keys = keys[:0]
				for k := range ref {
					keys = append(keys, k)
				}
			}
			m := fmt.Sprint("m", r.IntN(target*2))
			s := float64(r.IntN(1000))
			insert := r.IntN(10) < 7
			if !grow {
				// Shrink: mostly delete existing members.
				i := r.IntN(len(keys))
				m = keys[i]
				keys[i] = keys[len(keys)-1]
				keys = keys[:len(keys)-1]
				insert = r.IntN(10) < 1
				if insert {
					keys = append(keys, m)
				}
			}
			if old, ok := ref[m]; ok {
				if !tree.delete(zitem{old, m}) {
					t.Fatalf("seed %d: delete of present item failed", seed)
				}
				delete(ref, m)
			}
			if insert {
				tree.insert(zitem{s, m})
				ref[m] = s
			}
			ops++
			if ops%20000 == 0 {
				items := tree.check(t)
				want := make([]zitem, 0, len(ref))
				for k, v := range ref {
					want = append(want, zitem{v, k})
				}
				sort.Slice(want, func(i, j int) bool { return want[i].less(want[j]) })
				for i := range want {
					if items[i] != want[i] {
						t.Fatalf("seed %d op %d: item %d is %v, want %v", seed, ops, i, items[i], want[i])
					}
				}
				for k := 0; k < 50 && len(want) > 0; k++ {
					i := r.IntN(len(want))
					if got := tree.rank(want[i]); got != i {
						t.Fatalf("rank = %d, want %d", got, i)
					}
					if got := tree.at(i).item(); got != want[i] {
						t.Fatalf("at(%d) = %v", i, got)
					}
				}
				if tree.delete(zitem{-1, "absent"}) {
					t.Fatal("deleted absent item")
				}
			}
		}
		tree.check(t)
	}
}

func BenchmarkZTreeInsertDelete(b *testing.B) {
	tree := newZTree()
	members := make([]string, 100000)
	scores := make([]float64, len(members))
	for i := range members {
		members[i] = "element:" + fmt.Sprint(i)
		scores[i] = float64(rand.IntN(100000))
		tree.insert(zitem{scores[i], members[i]})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		j := rand.IntN(len(members))
		tree.delete(zitem{scores[j], members[j]})
		scores[j] = float64(rand.IntN(100000))
		tree.insert(zitem{scores[j], members[j]})
	}
}
