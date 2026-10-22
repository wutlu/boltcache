package cache

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// PersistOptions configures periodic snapshots.
type PersistOptions struct {
	File     string
	Interval time.Duration
	Compress bool
	// BackupCount is how many previous snapshots to keep next to File
	// (File.backup.<timestamp>). 0 disables backups.
	BackupCount int
	// CleanupWhenExceeds delays pruning until more than this many backups
	// exist, batching deletions. Values <= BackupCount prune on every save.
	CleanupWhenExceeds int
	// Logf receives informational messages; may be nil.
	Logf func(format string, args ...any)
}

// Persister periodically writes snapshots of a cache to disk.
type Persister struct {
	c    *BoltCache
	opts PersistOptions

	mu        sync.Mutex // serializes saves
	lastSaved uint64     // c.Writes() at the last successful save
	saved     bool
	lastTime  time.Time

	running  bool
	stopOnce sync.Once
	stop     chan struct{}
	done     chan struct{}
}

// NewPersister creates a persister; call Start to begin periodic saves.
func NewPersister(c *BoltCache, opts PersistOptions) *Persister {
	if opts.Interval <= 0 {
		opts.Interval = 30 * time.Second
	}
	if opts.Logf == nil {
		opts.Logf = func(string, ...any) {}
	}
	return &Persister{c: c, opts: opts, stop: make(chan struct{}), done: make(chan struct{})}
}

// Load restores the snapshot file into the cache.
func (p *Persister) Load() (int, error) {
	n, err := p.c.LoadFile(p.opts.File)
	if err != nil {
		return n, fmt.Errorf("load %s: %w", p.opts.File, err)
	}
	p.mu.Lock()
	p.lastSaved, p.saved = p.c.Writes(), true
	p.mu.Unlock()
	return n, nil
}

// Start begins saving every Interval.
func (p *Persister) Start() {
	p.running = true
	go func() {
		defer close(p.done)
		t := time.NewTicker(p.opts.Interval)
		defer t.Stop()
		for {
			select {
			case <-p.stop:
				return
			case <-t.C:
				if _, err := p.Save(false); err != nil {
					p.opts.Logf("persistence: %v", err)
				}
			}
		}
	}()
}

// Stop ends the periodic loop and writes a final snapshot.
func (p *Persister) Stop() error {
	p.stopOnce.Do(func() {
		close(p.stop)
		if p.running {
			<-p.done
		}
	})
	_, err := p.Save(false)
	return err
}

// Save writes a snapshot. Unless force is set it is skipped when nothing
// changed since the previous save. It reports whether a file was written.
func (p *Persister) Save(force bool) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	writes := p.c.Writes()
	if !force && p.saved && writes == p.lastSaved {
		return false, nil
	}
	if p.opts.BackupCount > 0 {
		if err := p.backup(); err != nil {
			p.opts.Logf("persistence: backup failed: %v", err)
		}
	}
	if _, err := p.c.SaveFile(p.opts.File, p.opts.Compress); err != nil {
		return false, err
	}
	p.lastSaved, p.saved, p.lastTime = writes, true, time.Now()
	if p.opts.BackupCount > 0 {
		p.prune()
	}
	return true, nil
}

// LastSave returns the time of the last successful save (zero if none).
func (p *Persister) LastSave() time.Time {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastTime
}

const backupMarker = ".backup."

// backup preserves the current snapshot file before it is replaced. A hard
// link is used when possible (no copy), falling back to copying.
func (p *Persister) backup() error {
	src := p.opts.File
	if _, err := os.Stat(src); err != nil {
		return nil // nothing to back up yet
	}
	dst := src + backupMarker + time.Now().Format("20060102-150405.000000")
	if err := os.Link(src, dst); err == nil {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// prune deletes the oldest backups once more than max(BackupCount,
// CleanupWhenExceeds) exist, keeping the newest BackupCount.
func (p *Persister) prune() {
	dir := filepath.Dir(p.opts.File)
	prefix := filepath.Base(p.opts.File) + backupMarker
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var backups []string
	for _, e := range ents {
		if !e.IsDir() && strings.HasPrefix(e.Name(), prefix) {
			backups = append(backups, e.Name())
		}
	}
	limit := max(p.opts.BackupCount, p.opts.CleanupWhenExceeds)
	if len(backups) <= limit {
		return
	}
	// Timestamps are fixed-width, so lexical order is chronological.
	sort.Strings(backups)
	excess := len(backups) - p.opts.BackupCount
	for _, name := range backups[:excess] {
		_ = os.Remove(filepath.Join(dir, name))
	}
}
