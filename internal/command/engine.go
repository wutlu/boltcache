// Package command implements BoltCache's Redis-compatible command set. A
// single Engine executes commands for every transport (RESP, the legacy text
// protocol, REST and Lua scripts), so they all behave identically.
package command

import (
	"crypto/subtle"
	"fmt"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wutlu/boltcache/v2/internal/cache"
	"github.com/wutlu/boltcache/v2/internal/pubsub"
	"github.com/wutlu/boltcache/v2/internal/resp"
)

// Authenticator validates AUTH passwords (API tokens).
type Authenticator interface {
	Enabled() bool
	Check(password string) bool
}

// Config controls optional engine features.
type Config struct {
	// Auth validates AUTH; nil (and no Passwords) disables authentication.
	Auth Authenticator
	// Passwords is a static alternative to Auth.
	Passwords []string
	// LuaEnabled enables EVAL/EVALSHA/SCRIPT.
	LuaEnabled bool
	// PubSubEnabled enables PUBLISH/SUBSCRIBE.
	PubSubEnabled bool
	// ScriptTimeout aborts scripts running longer than this (default 5s).
	ScriptTimeout time.Duration
	// Version is reported by INFO and HELLO.
	Version string
}

// Engine executes commands against a cache.
type Engine struct {
	cache   *cache.BoltCache
	hub     *pubsub.Hub
	cfg     Config
	scripts *scriptCache
	started time.Time

	// Optional hooks used by INFO/SAVE; set before serving.
	Persister   *cache.Persister
	ClientCount func() int64

	nextID   atomic.Int64
	commands stripedCounter
	rejected stripedCounter

	combine []combineQueue // per cache shard, see combineBatch
}

// New creates an engine.
func New(c *cache.BoltCache, hub *pubsub.Hub, cfg Config) *Engine {
	if cfg.ScriptTimeout <= 0 {
		cfg.ScriptTimeout = 5 * time.Second
	}
	if cfg.Version == "" {
		cfg.Version = "dev"
	}
	if hub == nil {
		hub = pubsub.NewHub()
	}
	return &Engine{cache: c, hub: hub, cfg: cfg, scripts: newScriptCache(), started: time.Now(),
		combine: make([]combineQueue, c.ShardCount())}
}

// Cache returns the underlying cache.
func (e *Engine) Cache() *cache.BoltCache { return e.cache }

// Hub returns the pub/sub hub.
func (e *Engine) Hub() *pubsub.Hub { return e.hub }

// AuthRequired reports whether clients must authenticate.
func (e *Engine) AuthRequired() bool {
	if e.cfg.Auth != nil {
		return e.cfg.Auth.Enabled()
	}
	return len(e.cfg.Passwords) > 0
}

// CheckPassword validates p in constant time.
func (e *Engine) CheckPassword(p string) bool {
	if e.cfg.Auth != nil {
		return e.cfg.Auth.Check(p)
	}
	ok := 0
	for _, want := range e.cfg.Passwords {
		ok |= subtle.ConstantTimeCompare([]byte(p), []byte(want))
	}
	return ok == 1
}

// Session holds per-connection state.
type Session struct {
	ID            int64
	Name          string
	Addr          string
	Authenticated bool
	// Subscriber receives pub/sub messages; nil when the transport doesn't
	// support subscriptions.
	Subscriber pubsub.Subscriber
	// Close is set when the client asked to close the connection (QUIT).
	Close bool

	channels map[string]struct{}
	patterns map[string]struct{}

	inMulti      bool
	multiAborted bool
	queue        [][]string
	watched      map[string]uint64 // key -> cache.KeyVersion at WATCH time

	tx    *cache.Tx // set while running inside Atomic
	batch bool      // tx is held for a pipeline batch (see ExecPipeline)
	// Scratch space reused by ExecPipeline.
	pipeSpecs  []*spec
	pipeShards []int
	combineReq *batchReq
	// While pipelining, executed commands are tallied in pipeCommands and
	// added to Engine.commands once at the end: an atomic add per command
	// costs more than a simple command itself.
	pipelining   bool
	pipeCommands uint64
	inScript     bool
	created      time.Time
}

// NewSession creates a session; it's pre-authenticated when no password is
// configured.
func (e *Engine) NewSession(addr string, sub pubsub.Subscriber) *Session {
	return &Session{
		ID:            e.nextID.Add(1),
		Addr:          addr,
		Subscriber:    sub,
		Authenticated: !e.AuthRequired(),
		created:       time.Now(),
	}
}

// Subscriptions returns the number of channel and pattern subscriptions.
func (s *Session) Subscriptions() int { return len(s.channels) + len(s.patterns) }

// CloseSession releases resources held by s (subscriptions).
func (e *Engine) CloseSession(s *Session) {
	if s.Subscriber == nil || s.Subscriptions() == 0 {
		return
	}
	chans := make([]string, 0, len(s.channels))
	for c := range s.channels {
		chans = append(chans, c)
	}
	pats := make([]string, 0, len(s.patterns))
	for p := range s.patterns {
		pats = append(pats, p)
	}
	e.hub.UnsubscribeAll(s.Subscriber, chans, pats)
	s.channels, s.patterns = nil, nil
}

// Exec runs one command (argv[0] is the name) and writes its reply to w.
func (e *Engine) Exec(s *Session, argv [][]byte, w resp.Writer) {
	if len(argv) == 0 {
		return
	}
	args := make([]string, len(argv))
	for i, a := range argv {
		args[i] = string(a)
	}
	e.ExecStrings(s, args, w)
}

// ExecStrings is Exec for already converted arguments.
func (e *Engine) ExecStrings(s *Session, args []string, w resp.Writer) {
	if len(args) == 0 {
		return
	}
	e.execSpec(s, lookup(args[0]), args, w)
}

// execSpec is ExecStrings with the command already looked up (sp may be
// nil for unknown commands).
func (e *Engine) execSpec(s *Session, sp *spec, args []string, w resp.Writer) {
	if sp == nil {
		e.rejected.add(s.ID)
		if s.inMulti {
			s.multiAborted = true
		}
		w.Error(unknownCommand(args))
		return
	}
	if !sp.arityOK(len(args)) {
		e.rejected.add(s.ID)
		if s.inMulti {
			s.multiAborted = true
		}
		w.Error(fmt.Sprintf("ERR wrong number of arguments for '%s' command", sp.name))
		return
	}
	if !s.Authenticated && sp.flags&flagNoAuth == 0 {
		e.rejected.add(s.ID)
		w.Error("NOAUTH Authentication required.")
		return
	}
	if s.Subscriptions() > 0 && sp.flags&flagPubSub == 0 {
		e.rejected.add(s.ID)
		w.Error(fmt.Sprintf("ERR Can't execute '%s': only (P)SUBSCRIBE / (P)UNSUBSCRIBE / PING / QUIT / RESET are allowed in this context", sp.name))
		return
	}
	if s.inMulti && sp.flags&flagMultiControl == 0 {
		if err := e.featureCheck(sp); err != "" {
			s.multiAborted = true
			w.Error(err)
			return
		}
		// Clone: transports may reuse the argument slice after Exec returns.
		s.queue = append(s.queue, slices.Clone(args))
		w.Simple("QUEUED")
		return
	}
	e.run(s, sp, args, w)
}

func unknownCommand(args []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "ERR unknown command '%s', with args beginning with: ", truncate(args[0], 128))
	for _, a := range args[1:] {
		if b.Len() > 256 {
			break
		}
		fmt.Fprintf(&b, "'%s' ", truncate(a, 64))
	}
	return strings.TrimSpace(b.String())
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func (e *Engine) featureCheck(sp *spec) string {
	if sp.flags&flagScripting != 0 && !e.cfg.LuaEnabled {
		return "ERR Lua scripting is disabled on this server (features.lua_scripting)"
	}
	if sp.flags&flagPubSubCmd != 0 && !e.cfg.PubSubEnabled {
		return "ERR Pub/Sub is disabled on this server (features.pub_sub)"
	}
	return ""
}

// run executes sp. Commands touching several keys run inside cache.Atomic so
// they're atomic like in Redis.
func (e *Engine) run(s *Session, sp *spec, args []string, w resp.Writer) {
	if msg := e.featureCheck(sp); msg != "" {
		w.Error(msg)
		return
	}
	// Inside MULTI/EXEC and scripts OOM was checked before starting; a
	// pipeline batch checks every command like the unbatched path does.
	if (s.tx == nil || s.batch) && sp.flags&flagDenyOOM != 0 && e.cache.OOM() {
		e.rejected.add(s.ID)
		w.Error(cache.ErrOOM.Error())
		return
	}
	if s.pipelining {
		s.pipeCommands++ // counted once per pipeline, see ExecPipeline
	} else {
		e.commands.add(s.ID)
	}
	if s.tx != nil {
		sp.fn(e, s, s.tx, args, w)
		return
	}
	if sp.flags&flagNoKeys == 0 {
		if sp.multiKey(args) {
			e.runLocked(s, sp, args, w, e.cache.Begin(sp.keys(args), false))
			return
		}
	}
	sp.fn(e, s, e.cache.Tx, args, w)
}

// ExecPipeline runs commands received together (a client pipeline).
// Consecutive single-key commands whose keys live in the same shard run
// under one lock acquisition instead of one each. Without this, many
// clients pipelining commands at one hot key (a leaderboard, a counter, a
// queue) spend most of their time contending for that shard's lock.
// Results and semantics are identical to calling ExecStrings for each
// command.
func (e *Engine) ExecPipeline(s *Session, cmds [][]string, w resp.Writer) {
	// Look every command up once; shard is -1 for commands that can't be
	// batched.
	if cap(s.pipeSpecs) < len(cmds) {
		s.pipeSpecs = make([]*spec, len(cmds))
		s.pipeShards = make([]int, len(cmds))
	}
	specs, shards := s.pipeSpecs[:len(cmds)], s.pipeShards[:len(cmds)]
	s.pipelining = true
	defer func() {
		s.pipelining = false
		if s.pipeCommands > 0 {
			e.commands.addN(s.ID, s.pipeCommands)
			s.pipeCommands = 0
		}
	}()
	for i, c := range cmds {
		if len(c) == 0 {
			shards[i] = -1
			continue
		}
		specs[i] = lookup(c[0])
		shards[i] = -1
		if key, ok := e.batchable(s, specs[i], c); ok {
			shards[i] = e.cache.ShardOf(key)
		}
	}
	for i := 0; i < len(cmds); {
		if s.Close {
			return
		}
		j := i + 1
		if shards[i] >= 0 {
			for j < len(cmds) && shards[j] == shards[i] {
				j++
			}
		}
		if j-i == 1 {
			if len(cmds[i]) > 0 {
				e.execSpec(s, specs[i], cmds[i], w)
			}
			i++
			continue
		}
		key, _ := specs[i].singleKey(cmds[i])
		e.combineBatch(s, shards[i], key, specs[i:j], cmds[i:j], w)
		i = j
	}
}

// runLocked runs sp with s holding tx (from cache.Begin), releasing it
// afterwards even if the command panics.
func (e *Engine) runLocked(s *Session, sp *spec, args []string, w resp.Writer, tx *cache.Tx) {
	s.tx = tx
	defer func() {
		s.tx = nil
		tx.End()
	}()
	sp.fn(e, s, tx, args, w)
}

// runBatchTx runs a pipeline batch whose commands all live in tx's shard.
func (e *Engine) runBatchTx(s *Session, specs []*spec, cmds [][]string, w resp.Writer, tx *cache.Tx) {
	s.tx, s.batch = tx, true
	defer func() { s.tx, s.batch = nil, false }()
	for k := range cmds {
		e.execSpec(s, specs[k], cmds[k], w)
	}
}

// batchable reports whether args is a plain single-key command that can
// share a lock with its neighbours, and returns its key. Session state
// (auth, MULTI, subscriptions) is checked before every command runs, so a
// command that changes it simply ends the batch.
func (e *Engine) batchable(s *Session, sp *spec, args []string) (string, bool) {
	if sp == nil || !s.Authenticated || s.inMulti || s.Subscriptions() > 0 {
		return "", false
	}
	const excluded = flagNoKeys | flagNoTx | flagGlobal | flagScripting | flagMultiControl | flagPubSub
	if sp.flags&excluded != 0 || !sp.arityOK(len(args)) {
		return "", false
	}
	return sp.singleKey(args)
}

// Stats about command processing.
func (e *Engine) Stats() (commands, rejected uint64) {
	return e.commands.load(), e.rejected.load()
}

// writeErr writes err using Redis error conventions.
func writeErr(w resp.Writer, err error) {
	w.Error(err.Error())
}

// Hot-shard batches use flat combining: while a shard is busy, other event
// loops don't wait for its lock but queue their batch, and whoever holds
// the lock runs the queued batches before releasing it. The shard's data
// then stays in one core's cache instead of bouncing between cores with
// every lock handoff, which is what limits a hot key (one leaderboard, one
// queue) under many concurrent clients.

// combineQueue holds the batches waiting for one shard.
type combineQueue struct {
	mu    sync.Mutex
	reqs  []*batchReq
	spare []*batchReq
	_     [64]byte
}

// batchReq is a pipeline batch handed to a combiner. Its owner spins until
// done is set; the combiner writes the replies to w.
type batchReq struct {
	s     *Session
	specs []*spec
	cmds  [][]string
	w     resp.Writer
	done  atomic.Bool
	panic any
}

// combineMaxRounds bounds how many queue drains a combiner does before
// returning to its own connection.
const combineMaxRounds = 8

func (e *Engine) combineBatch(s *Session, shard int, key string, specs []*spec, cmds [][]string, w resp.Writer) {
	if tx := e.cache.TryBeginKey(key); tx != nil {
		func() {
			defer tx.End()
			e.runBatchTx(s, specs, cmds, w, tx)
			e.drain(shard, tx)
		}()
		return
	}
	if s.combineReq == nil {
		s.combineReq = new(batchReq)
	}
	req := s.combineReq
	req.s, req.specs, req.cmds, req.w, req.panic = s, specs, cmds, w, nil
	req.done.Store(false)
	q := &e.combine[shard]
	q.mu.Lock()
	q.reqs = append(q.reqs, req)
	q.mu.Unlock()
	for spins := 0; !req.done.Load(); spins++ {
		if tx := e.cache.TryBeginKey(key); tx != nil {
			func() {
				defer tx.End()
				e.drain(shard, tx)
			}()
			continue
		}
		if spins > 64 {
			runtime.Gosched()
		}
	}
	p := req.panic
	req.s, req.specs, req.cmds, req.w, req.panic = nil, nil, nil, nil, nil
	if p != nil {
		panic(p)
	}
}

// drain runs queued batches for shard while holding its lock (tx).
func (e *Engine) drain(shard int, tx *cache.Tx) {
	q := &e.combine[shard]
	for round := 0; round < combineMaxRounds; round++ {
		q.mu.Lock()
		reqs := q.reqs
		if len(reqs) == 0 {
			q.mu.Unlock()
			return
		}
		q.reqs, q.spare = q.spare[:0], nil
		q.mu.Unlock()
		for i, r := range reqs {
			e.runQueued(r, tx)
			reqs[i] = nil
		}
		q.mu.Lock()
		if q.spare == nil {
			q.spare = reqs[:0]
		}
		q.mu.Unlock()
	}
}

// runQueued runs r for its owner; a panic is handed to the owner so the
// right connection is closed.
func (e *Engine) runQueued(r *batchReq, tx *cache.Tx) {
	defer func() {
		if p := recover(); p != nil {
			r.s.tx, r.s.batch = nil, false
			r.panic = p
		}
		r.done.Store(true)
	}()
	e.runBatchTx(r.s, r.specs, r.cmds, r.w, tx)
}
