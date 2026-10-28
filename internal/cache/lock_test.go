package cache

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRWLockExclusion(t *testing.T) {
	var (
		l       rwLock
		readers atomic.Int32
		writers atomic.Int32
		counter int // guarded by l
		wg      sync.WaitGroup
	)
	const workers, iters = 8, 20000
	for w := 0; w < workers; w++ {
		wg.Add(2)
		go func() { // writer
			defer wg.Done()
			for i := 0; i < iters; i++ {
				if i%2 == 0 {
					l.Lock()
				} else {
					for !l.TryLock() {
					}
				}
				if writers.Add(1) != 1 || readers.Load() != 0 {
					t.Error("writer not exclusive")
				}
				counter++
				writers.Add(-1)
				l.Unlock()
			}
		}()
		go func() { // reader
			defer wg.Done()
			for i := 0; i < iters; i++ {
				l.RLock()
				readers.Add(1)
				if writers.Load() != 0 {
					t.Error("reader overlaps a writer")
				}
				_ = counter
				readers.Add(-1)
				l.RUnlock()
			}
		}()
	}
	wg.Wait()
	if counter != workers*iters {
		t.Fatalf("counter = %d, want %d", counter, workers*iters)
	}
	if s := l.state.Load(); s != 0 {
		t.Fatalf("state = %d after all unlocks", s)
	}
}

// A steady stream of readers must not starve a writer.
func TestRWLockWriterNotStarved(t *testing.T) {
	var l rwLock
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				l.RLock()
				time.Sleep(10 * time.Microsecond)
				l.RUnlock()
			}
		}()
	}
	time.Sleep(5 * time.Millisecond)
	done := make(chan struct{})
	go func() { l.Lock(); l.Unlock(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("writer starved by readers")
	}
	close(stop)
	wg.Wait()
}
