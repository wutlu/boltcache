package cache

// deque is a growable ring buffer of strings giving O(1) push/pop at both
// ends, which is what LPUSH/RPUSH/LPOP/RPOP need. Index 0 is the list head.
type deque struct {
	buf  []string
	head int
	n    int
}

func (d *deque) Len() int { return d.n }

func (d *deque) grow() {
	size := len(d.buf) * 2
	if size == 0 {
		size = 4
	}
	nb := make([]string, size)
	d.copyTo(nb)
	d.buf = nb
	d.head = 0
}

// copyTo copies the elements in order into dst, which must have room for n.
func (d *deque) copyTo(dst []string) {
	if d.n == 0 {
		return
	}
	end := d.head + d.n
	if end <= len(d.buf) {
		copy(dst, d.buf[d.head:end])
		return
	}
	k := copy(dst, d.buf[d.head:])
	copy(dst[k:], d.buf[:end-len(d.buf)])
}

func (d *deque) idx(i int) int {
	j := d.head + i
	if j >= len(d.buf) {
		j -= len(d.buf)
	}
	return j
}

func (d *deque) PushFront(v string) {
	if d.n == len(d.buf) {
		d.grow()
	}
	d.head--
	if d.head < 0 {
		d.head += len(d.buf)
	}
	d.buf[d.head] = v
	d.n++
}

func (d *deque) PushBack(v string) {
	if d.n == len(d.buf) {
		d.grow()
	}
	d.buf[d.idx(d.n)] = v
	d.n++
}

func (d *deque) PopFront() string {
	v := d.buf[d.head]
	d.buf[d.head] = ""
	d.head = d.idx(1)
	d.n--
	d.maybeShrink()
	return v
}

func (d *deque) PopBack() string {
	j := d.idx(d.n - 1)
	v := d.buf[j]
	d.buf[j] = ""
	d.n--
	d.maybeShrink()
	return v
}

func (d *deque) maybeShrink() {
	if len(d.buf) > 64 && d.n < len(d.buf)/4 {
		nb := make([]string, len(d.buf)/2)
		d.copyTo(nb)
		d.buf = nb
		d.head = 0
	}
}

func (d *deque) At(i int) string     { return d.buf[d.idx(i)] }
func (d *deque) Set(i int, v string) { d.buf[d.idx(i)] = v }

// Slice returns a copy of elements [start, stop] (inclusive, already clamped).
func (d *deque) Slice(start, stop int) []string {
	if start > stop {
		return []string{}
	}
	out := make([]string, 0, stop-start+1)
	for i := start; i <= stop; i++ {
		out = append(out, d.At(i))
	}
	return out
}

// Values returns a copy of every element in order.
func (d *deque) Values() []string {
	out := make([]string, d.n)
	d.copyTo(out)
	return out
}

// Replace rebuilds the deque from vals.
func (d *deque) Replace(vals []string) {
	d.buf = vals
	d.head = 0
	d.n = len(vals)
}
