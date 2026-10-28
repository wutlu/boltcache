package cache

import "sort"

// ztree is the ordered index of a sorted set: a counted B+tree of
// (score, member) items. Leaves hold up to ztMax items in a contiguous
// slice and are linked for iteration; internal nodes keep each child's
// subtree size (for O(log n) ranks) and largest item (for routing).
//
// Compared with a skiplist (Redis' choice), a lookup touches a handful of
// contiguous nodes instead of dozens of scattered ones, which matters far
// more than comparison counts on modern CPUs.
type ztree struct {
	root   *ztNode
	length int
}

type zitem struct {
	score  float64
	member string
}

func (a zitem) less(b zitem) bool {
	return a.score < b.score || (a.score == b.score && a.member < b.member)
}

type ztNode struct {
	// leaf
	items      []zitem
	next, prev *ztNode
	// internal
	children []*ztNode
	counts   []int
	maxs     []zitem
}

const (
	ztMax = 64 // max items per leaf / children per internal node
	ztMin = ztMax / 4
)

func (n *ztNode) leaf() bool { return n.children == nil }

func (n *ztNode) size() int {
	if n.leaf() {
		return len(n.items)
	}
	s := 0
	for _, c := range n.counts {
		s += c
	}
	return s
}

func (n *ztNode) max() zitem {
	if n.leaf() {
		return n.items[len(n.items)-1]
	}
	return n.maxs[len(n.maxs)-1]
}

// route returns the child that holds (or would hold) it.
func (n *ztNode) route(it zitem) int {
	i := sort.Search(len(n.maxs), func(i int) bool { return !n.maxs[i].less(it) })
	if i == len(n.maxs) {
		i--
	}
	return i
}

func newZTree() *ztree { return &ztree{root: &ztNode{}} }

func (t *ztree) insert(it zitem) {
	if right := t.insertAt(t.root, it); right != nil {
		left := t.root
		t.root = &ztNode{
			children: []*ztNode{left, right},
			counts:   []int{left.size(), right.size()},
			maxs:     []zitem{left.max(), right.max()},
		}
	}
	t.length++
}

// insertAt inserts it below n and returns a new right sibling if n split.
func (t *ztree) insertAt(n *ztNode, it zitem) *ztNode {
	if n.leaf() {
		i := sort.Search(len(n.items), func(i int) bool { return !n.items[i].less(it) })
		n.items = append(n.items, zitem{})
		copy(n.items[i+1:], n.items[i:])
		n.items[i] = it
		if len(n.items) <= ztMax {
			return nil
		}
		half := len(n.items) / 2
		right := &ztNode{items: append(make([]zitem, 0, ztMax+1), n.items[half:]...)}
		clear(n.items[half:])
		n.items = n.items[:half]
		right.next, right.prev = n.next, n
		if n.next != nil {
			n.next.prev = right
		}
		n.next = right
		return right
	}
	i := n.route(it)
	right := t.insertAt(n.children[i], it)
	if right == nil {
		n.counts[i]++
		if n.maxs[i].less(it) {
			n.maxs[i] = it
		}
		return nil
	}
	left := n.children[i]
	n.counts[i], n.maxs[i] = left.size(), left.max()
	n.children = append(n.children, nil)
	copy(n.children[i+2:], n.children[i+1:])
	n.children[i+1] = right
	n.counts = append(n.counts, 0)
	copy(n.counts[i+2:], n.counts[i+1:])
	n.counts[i+1] = right.size()
	n.maxs = append(n.maxs, zitem{})
	copy(n.maxs[i+2:], n.maxs[i+1:])
	n.maxs[i+1] = right.max()
	if len(n.children) <= ztMax {
		return nil
	}
	half := len(n.children) / 2
	r := &ztNode{
		children: append([]*ztNode(nil), n.children[half:]...),
		counts:   append([]int(nil), n.counts[half:]...),
		maxs:     append([]zitem(nil), n.maxs[half:]...),
	}
	clear(n.children[half:])
	n.children, n.counts, n.maxs = n.children[:half], n.counts[:half], n.maxs[:half]
	return r
}

// delete removes it and reports whether it was present.
func (t *ztree) delete(it zitem) bool {
	if !t.deleteAt(t.root, it) {
		return false
	}
	t.length--
	for !t.root.leaf() && len(t.root.children) == 1 {
		t.root = t.root.children[0]
	}
	return true
}

func (t *ztree) deleteAt(n *ztNode, it zitem) bool {
	if n.leaf() {
		i := sort.Search(len(n.items), func(i int) bool { return !n.items[i].less(it) })
		if i == len(n.items) || n.items[i] != it {
			return false
		}
		copy(n.items[i:], n.items[i+1:])
		n.items[len(n.items)-1] = zitem{}
		n.items = n.items[:len(n.items)-1]
		return true
	}
	i := n.route(it)
	child := n.children[i]
	if !t.deleteAt(child, it) {
		return false
	}
	n.counts[i]--
	if n.counts[i] == 0 {
		n.removeChild(i)
		return true
	}
	if n.maxs[i] == it {
		n.maxs[i] = child.max()
	}
	// Merge a small child into a neighbour so nodes stay reasonably full.
	if child.size() < ztMin && len(n.children) > 1 {
		j := i + 1
		if j == len(n.children) {
			i, j = i-1, i
		}
		n.tryMerge(i, j)
	}
	return true
}

// removeChild drops an empty child i.
func (n *ztNode) removeChild(i int) {
	c := n.children[i]
	if c.leaf() {
		if c.prev != nil {
			c.prev.next = c.next
		}
		if c.next != nil {
			c.next.prev = c.prev
		}
	}
	n.children = append(n.children[:i], n.children[i+1:]...)
	n.counts = append(n.counts[:i], n.counts[i+1:]...)
	n.maxs = append(n.maxs[:i], n.maxs[i+1:]...)
}

// tryMerge folds child j into child i (i < j, adjacent) if they fit.
func (n *ztNode) tryMerge(i, j int) {
	a, b := n.children[i], n.children[j]
	if a.leaf() {
		if len(a.items)+len(b.items) > ztMax {
			return
		}
		a.items = append(a.items, b.items...)
		a.next = b.next
		if b.next != nil {
			b.next.prev = a
		}
	} else {
		if len(a.children)+len(b.children) > ztMax {
			return
		}
		a.children = append(a.children, b.children...)
		a.counts = append(a.counts, b.counts...)
		a.maxs = append(a.maxs, b.maxs...)
	}
	n.counts[i] += n.counts[j]
	n.maxs[i] = n.maxs[j]
	n.children = append(n.children[:j], n.children[j+1:]...)
	n.counts = append(n.counts[:j], n.counts[j+1:]...)
	n.maxs = append(n.maxs[:j], n.maxs[j+1:]...)
}

// zpos is a position in the tree: an item index within a leaf.
type zpos struct {
	leaf *ztNode
	i    int
}

func (p zpos) valid() bool { return p.leaf != nil }
func (p zpos) item() zitem { return p.leaf.items[p.i] }
func (p zpos) next() zpos  { return p.step(1) }
func (p zpos) prev() zpos  { return p.step(-1) }
func (p zpos) step(d int) zpos {
	p.i += d
	for p.leaf != nil && (p.i < 0 || p.i >= len(p.leaf.items)) {
		if p.i < 0 {
			p.leaf = p.leaf.prev
			if p.leaf != nil {
				p.i = len(p.leaf.items) - 1
			}
		} else {
			p.leaf = p.leaf.next
			p.i = 0
		}
	}
	return p
}

// rank returns the 0-based rank of it, or -1 if absent.
func (t *ztree) rank(it zitem) int {
	r := 0
	n := t.root
	for !n.leaf() {
		i := n.route(it)
		for _, c := range n.counts[:i] {
			r += c
		}
		n = n.children[i]
	}
	i := sort.Search(len(n.items), func(i int) bool { return !n.items[i].less(it) })
	if i == len(n.items) || n.items[i] != it {
		return -1
	}
	return r + i
}

// at returns the position of rank r.
func (t *ztree) at(r int) zpos {
	if r < 0 || r >= t.length {
		return zpos{}
	}
	n := t.root
	for !n.leaf() {
		i := 0
		for r >= n.counts[i] {
			r -= n.counts[i]
			i++
		}
		n = n.children[i]
	}
	return zpos{n, r}
}

// seek returns the first position whose item satisfies pred, where pred is
// monotonic (false...false, true...true) over the ordering.
func (t *ztree) seek(pred func(zitem) bool) zpos {
	n := t.root
	for !n.leaf() {
		i := sort.Search(len(n.maxs), func(i int) bool { return pred(n.maxs[i]) })
		if i == len(n.maxs) {
			return zpos{}
		}
		n = n.children[i]
	}
	i := sort.Search(len(n.items), func(i int) bool { return pred(n.items[i]) })
	if i == len(n.items) {
		// Empty leaf (only possible for an empty tree).
		return zpos{n, i}.step(0)
	}
	return zpos{n, i}
}

// firstInRange returns the first item with a score within [min, max].
func (t *ztree) firstInRange(min, max ScoreBound) zpos {
	p := t.seek(func(it zitem) bool { return min.aboveMin(it.score) })
	if !p.valid() || !max.belowMax(p.item().score) {
		return zpos{}
	}
	return p
}

// lastInRange returns the last item with a score within [min, max].
func (t *ztree) lastInRange(min, max ScoreBound) zpos {
	p := t.seek(func(it zitem) bool { return !max.belowMax(it.score) })
	if p.valid() {
		p = p.prev()
	} else {
		p = t.at(t.length - 1)
	}
	if !p.valid() || !min.aboveMin(p.item().score) {
		return zpos{}
	}
	return p
}

// ScoreBound is one end of a score range: a value that is inclusive unless
// Exclusive is set ("(1.5" in Redis syntax).
type ScoreBound struct {
	Value     float64
	Exclusive bool
}

func (b ScoreBound) aboveMin(s float64) bool {
	if b.Exclusive {
		return s > b.Value
	}
	return s >= b.Value
}

func (b ScoreBound) belowMax(s float64) bool {
	if b.Exclusive {
		return s < b.Value
	}
	return s <= b.Value
}
