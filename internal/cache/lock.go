package cache

import (
	"runtime"
	"sync/atomic"
	"time"
)

// rwLock is the shard lock: a reader/writer spin lock. Shard critical
// sections take well under a microsecond, so instead of parking like
// sync.RWMutex, waiters spin, then yield, then sleep briefly. Its fast paths
// also need fewer atomic read-modify-write instructions than sync.RWMutex
// (one per write lock/unlock pair instead of four), and on the hot path
// those cost more than the rest of a simple command: each one waits for the
// CPU's pending stores to drain.
//
// state is -1 while a writer holds the lock, otherwise the number of
// readers, plus writerWaiting while a writer is waiting: new readers then
// back off so writers aren't starved by a stream of readers.
type rwLock struct {
	state atomic.Int32
}

const writerWaiting = 1 << 30

func (l *rwLock) TryLock() bool {
	s := l.state.Load()
	return (s == 0 || s == writerWaiting) && l.state.CompareAndSwap(s, -1)
}

func (l *rwLock) Lock() {
	for i := 0; ; i++ {
		s := l.state.Load()
		switch {
		case s == 0 || s == writerWaiting:
			if l.state.CompareAndSwap(s, -1) {
				return
			}
			continue
		case s > 0 && s&writerWaiting == 0:
			l.state.CompareAndSwap(s, s|writerWaiting)
		}
		backoff(i)
	}
}

// Unlock releases a write lock. It clears writerWaiting: other waiting
// writers set it again on their next attempt.
func (l *rwLock) Unlock() { l.state.Store(0) }

func (l *rwLock) TryRLock() bool {
	s := l.state.Load()
	return s >= 0 && s&writerWaiting == 0 && l.state.CompareAndSwap(s, s+1)
}

func (l *rwLock) RLock() {
	for i := 0; !l.TryRLock(); i++ {
		backoff(i)
	}
}

func (l *rwLock) RUnlock() { l.state.Add(-1) }

// backoff waits before attempt i+1 to take a lock: busy-spin first, then
// yield the processor, then sleep, so a long holder (a Lua script, FLUSHALL)
// doesn't keep waiters burning CPU.
func backoff(i int) {
	switch {
	case i < 64:
		for j := 0; j < 16; j++ {
			spinHint()
		}
	case i < 256:
		runtime.Gosched()
	default:
		time.Sleep(20 * time.Microsecond)
	}
}

var spinSink atomic.Int32

// spinHint is a short pause in a spin loop that the compiler can't remove.
func spinHint() { _ = spinSink.Load() }
