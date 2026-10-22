// Package command implements BoltCache's Redis-compatible command set. A
// single Engine executes commands for every transport (RESP, the legacy text
// protocol, REST and Lua scripts), so they all behave identically.
package command

import (
	"crypto/subtle"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/wutlu/boltcache/internal/cache"
	"github.com/wutlu/boltcache/internal/pubsub"
	"github.com/wutlu/boltcache/internal/resp"
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
	commands atomic.Uint64
	rejected atomic.Uint64
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
	return &Engine{cache: c, hub: hub, cfg: cfg, scripts: newScriptCache(), started: time.Now()}
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

	tx       *cache.Tx // set while running inside Atomic
	inScript bool
	created  time.Time
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
	sp := lookup(args[0])
	if sp == nil {
		e.rejected.Add(1)
		if s.inMulti {
			s.multiAborted = true
		}
		w.Error(unknownCommand(args))
		return
	}
	if !sp.arityOK(len(args)) {
		e.rejected.Add(1)
		if s.inMulti {
			s.multiAborted = true
		}
		w.Error(fmt.Sprintf("ERR wrong number of arguments for '%s' command", sp.name))
		return
	}
	if !s.Authenticated && sp.flags&flagNoAuth == 0 {
		e.rejected.Add(1)
		w.Error("NOAUTH Authentication required.")
		return
	}
	if s.Subscriptions() > 0 && sp.flags&flagPubSub == 0 {
		e.rejected.Add(1)
		w.Error(fmt.Sprintf("ERR Can't execute '%s': only (P)SUBSCRIBE / (P)UNSUBSCRIBE / PING / QUIT / RESET are allowed in this context", sp.name))
		return
	}
	if s.inMulti && sp.flags&flagMultiControl == 0 {
		if err := e.featureCheck(sp); err != "" {
			s.multiAborted = true
			w.Error(err)
			return
		}
		s.queue = append(s.queue, args)
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
	e.commands.Add(1)
	if s.tx != nil {
		sp.fn(e, s, s.tx, args, w)
		return
	}
	if sp.flags&flagNoKeys == 0 {
		if keys := sp.keys(args); len(keys) > 1 {
			err := e.cache.Atomic(keys, false, func(tx *cache.Tx) error {
				s.tx = tx
				defer func() { s.tx = nil }()
				sp.fn(e, s, tx, args, w)
				return nil
			})
			if err != nil {
				w.Error(err.Error())
			}
			return
		}
	}
	sp.fn(e, s, e.cache.Tx, args, w)
}

// Stats about command processing.
func (e *Engine) Stats() (commands, rejected uint64) {
	return e.commands.Load(), e.rejected.Load()
}

// writeErr writes err using Redis error conventions.
func writeErr(w resp.Writer, err error) {
	w.Error(err.Error())
}
