package command

import "sync/atomic"

// stripedCounter spreads increments over cache-line-padded cells chosen by
// session ID, so event loops on different cores don't all write the same
// cache line on every command.
type stripedCounter struct {
	cells [64]struct {
		n atomic.Uint64
		_ [56]byte
	}
}

func (c *stripedCounter) add(id int64) { c.cells[uint64(id)%64].n.Add(1) }

func (c *stripedCounter) load() uint64 {
	var total uint64
	for i := range c.cells {
		total += c.cells[i].n.Load()
	}
	return total
}
