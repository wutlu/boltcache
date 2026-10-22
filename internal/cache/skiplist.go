package cache

import "math/rand/v2"

// skiplist keeps sorted set members ordered by (score, member). Like Redis'
// zskiplist, every forward link stores its span (the number of nodes it
// skips), so rank lookups and rank ranges are O(log n).
type skiplist struct {
	header *zNode
	tail   *zNode
	length int
	level  int
}

type zNode struct {
	member   string
	score    float64
	backward *zNode
	level    []zLevel
}

type zLevel struct {
	forward *zNode
	span    int
}

const (
	zMaxLevel = 32
	zP        = 0.25
)

// Nodes and their level arrays are allocated together so following a
// forward link touches one memory block, not two (most nodes have 1-2
// levels). This halves cache misses on large sets.
type zNode1 struct {
	zNode
	arr [1]zLevel
}

type zNode2 struct {
	zNode
	arr [2]zLevel
}

type zNode3 struct {
	zNode
	arr [3]zLevel
}

type zNode4 struct {
	zNode
	arr [4]zLevel
}

func newZNode(member string, score float64, lvl int) *zNode {
	switch lvl {
	case 1:
		n := &zNode1{zNode: zNode{member: member, score: score}}
		n.level = n.arr[:]
		return &n.zNode
	case 2:
		n := &zNode2{zNode: zNode{member: member, score: score}}
		n.level = n.arr[:]
		return &n.zNode
	case 3:
		n := &zNode3{zNode: zNode{member: member, score: score}}
		n.level = n.arr[:]
		return &n.zNode
	case 4:
		n := &zNode4{zNode: zNode{member: member, score: score}}
		n.level = n.arr[:]
		return &n.zNode
	}
	return &zNode{member: member, score: score, level: make([]zLevel, lvl)}
}

func newSkiplist() *skiplist {
	return &skiplist{header: &zNode{level: make([]zLevel, zMaxLevel)}, level: 1}
}

func randomLevel() int {
	l := 1
	for l < zMaxLevel && rand.Float64() < zP {
		l++
	}
	return l
}

// before reports whether n sorts before (score, member): by score, then
// member.
func (n *zNode) before(score float64, member string) bool {
	return n.score < score || (n.score == score && n.member < member)
}

func (z *skiplist) insert(score float64, member string) *zNode {
	var update [zMaxLevel]*zNode
	var rank [zMaxLevel]int
	x := z.header
	for i := z.level - 1; i >= 0; i-- {
		if i < z.level-1 {
			rank[i] = rank[i+1]
		}
		for f := x.level[i].forward; f != nil && f.before(score, member); f = x.level[i].forward {
			rank[i] += x.level[i].span
			x = f
		}
		update[i] = x
	}
	lvl := randomLevel()
	if lvl > z.level {
		for i := z.level; i < lvl; i++ {
			rank[i] = 0
			update[i] = z.header
			update[i].level[i].span = z.length
		}
		z.level = lvl
	}
	n := newZNode(member, score, lvl)
	for i := 0; i < lvl; i++ {
		n.level[i].forward = update[i].level[i].forward
		update[i].level[i].forward = n
		n.level[i].span = update[i].level[i].span - (rank[0] - rank[i])
		update[i].level[i].span = rank[0] - rank[i] + 1
	}
	for i := lvl; i < z.level; i++ {
		update[i].level[i].span++
	}
	if update[0] != z.header {
		n.backward = update[0]
	}
	if n.level[0].forward != nil {
		n.level[0].forward.backward = n
	} else {
		z.tail = n
	}
	z.length++
	return n
}

func (z *skiplist) deleteNode(x *zNode, update *[zMaxLevel]*zNode) {
	for i := 0; i < z.level; i++ {
		if update[i].level[i].forward == x {
			update[i].level[i].span += x.level[i].span - 1
			update[i].level[i].forward = x.level[i].forward
		} else {
			update[i].level[i].span--
		}
	}
	if x.level[0].forward != nil {
		x.level[0].forward.backward = x.backward
	} else {
		z.tail = x.backward
	}
	for z.level > 1 && z.header.level[z.level-1].forward == nil {
		z.level--
	}
	z.length--
}

// delete removes the node with the given score and member.
func (z *skiplist) delete(score float64, member string) bool {
	var update [zMaxLevel]*zNode
	x := z.header
	for i := z.level - 1; i >= 0; i-- {
		for f := x.level[i].forward; f != nil && f.before(score, member); f = x.level[i].forward {
			x = f
		}
		update[i] = x
	}
	x = x.level[0].forward
	if x != nil && x.score == score && x.member == member {
		z.deleteNode(x, &update)
		return true
	}
	return false
}

// rank returns the 0-based rank of (score, member), or -1.
func (z *skiplist) rank(score float64, member string) int {
	r := 0
	x := z.header
	for i := z.level - 1; i >= 0; i-- {
		for f := x.level[i].forward; f != nil && (f.score < score || (f.score == score && f.member <= member)); f = x.level[i].forward {
			r += x.level[i].span
			x = f
		}
		if x != z.header && x.member == member {
			return r - 1
		}
	}
	return -1
}

// byRank returns the node at 0-based rank r.
func (z *skiplist) byRank(r int) *zNode {
	if r < 0 || r >= z.length {
		return nil
	}
	target := r + 1
	traversed := 0
	x := z.header
	for i := z.level - 1; i >= 0; i-- {
		for x.level[i].forward != nil && traversed+x.level[i].span <= target {
			traversed += x.level[i].span
			x = x.level[i].forward
		}
		if traversed == target {
			return x
		}
	}
	return nil
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

// firstInRange returns the first node with a score within [min, max].
func (z *skiplist) firstInRange(min, max ScoreBound) *zNode {
	x := z.header
	for i := z.level - 1; i >= 0; i-- {
		for f := x.level[i].forward; f != nil && !min.aboveMin(f.score); f = x.level[i].forward {
			x = f
		}
	}
	x = x.level[0].forward
	if x == nil || !max.belowMax(x.score) {
		return nil
	}
	return x
}

// lastInRange returns the last node with a score within [min, max].
func (z *skiplist) lastInRange(min, max ScoreBound) *zNode {
	x := z.header
	for i := z.level - 1; i >= 0; i-- {
		for f := x.level[i].forward; f != nil && max.belowMax(f.score); f = x.level[i].forward {
			x = f
		}
	}
	if x == z.header || !min.aboveMin(x.score) {
		return nil
	}
	return x
}
