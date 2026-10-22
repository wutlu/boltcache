package command

import (
	"sort"
	"strings"

	"github.com/wutlu/boltcache/internal/cache"
	"github.com/wutlu/boltcache/internal/resp"
)

type handler func(e *Engine, s *Session, db *cache.Tx, args []string, w resp.Writer)

type flag uint32

const (
	flagWrite        flag = 1 << iota // modifies data
	flagReadOnly                      // only reads data
	flagNoAuth                        // allowed before AUTH
	flagPubSub                        // allowed while subscribed
	flagMultiControl                  // MULTI/EXEC/DISCARD/WATCH: never queued
	flagNoScript                      // not callable from Lua
	flagGlobal                        // touches the whole keyspace (needs all shards in a transaction)
	flagNoTx                          // must run outside shard locks (INFO, SAVE...)
	flagNoKeys                        // never auto-wrapped in Atomic
	flagScripting                     // requires features.lua_scripting
	flagPubSubCmd                     // requires features.pub_sub
	flagAdmin                         // administrative
	flagFast                          // O(1)/O(log n)
)

type spec struct {
	name  string
	fn    handler
	arity int // >0 exact, <0 minimum (absolute value), like Redis
	flags flag
	// Key positions (Redis convention): first and last argument index of
	// keys and the step between them; last < 0 counts from the end.
	first, last, step int
	// getKeys overrides the positional key spec (EVAL, ...).
	getKeys func(args []string) []string
}

func (sp *spec) arityOK(n int) bool {
	if sp.arity >= 0 {
		return n == sp.arity
	}
	return n >= -sp.arity
}

func (sp *spec) keys(args []string) []string {
	if sp.getKeys != nil {
		return sp.getKeys(args)
	}
	if sp.first <= 0 || sp.first >= len(args) {
		return nil
	}
	last := sp.last
	if last < 0 {
		last += len(args)
	}
	if last >= len(args) {
		last = len(args) - 1
	}
	step := sp.step
	if step <= 0 {
		step = 1
	}
	var out []string
	for i := sp.first; i <= last; i += step {
		out = append(out, args[i])
	}
	return out
}

var table = map[string]*spec{}

func register(name string, fn handler, arity int, flags flag, first, last, step int) *spec {
	sp := &spec{name: name, fn: fn, arity: arity, flags: flags, first: first, last: last, step: step}
	if first == 0 {
		sp.flags |= flagNoKeys
	}
	table[name] = sp
	return sp
}

// lookup finds a command by case-insensitive name.
func lookup(name string) *spec {
	if sp, ok := table[name]; ok {
		return sp
	}
	if len(name) > 32 {
		return nil
	}
	var buf [32]byte
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		buf[i] = c
	}
	return table[string(buf[:len(name)])]
}

// Names returns all command names, sorted.
func Names() []string {
	out := make([]string, 0, len(table))
	for n := range table {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// IsWrite reports whether the named command modifies data.
func IsWrite(name string) bool {
	sp := lookup(name)
	return sp != nil && sp.flags&flagWrite != 0
}

func (sp *spec) flagNames() []string {
	var out []string
	add := func(f flag, n string) {
		if sp.flags&f != 0 {
			out = append(out, n)
		}
	}
	add(flagWrite, "write")
	add(flagReadOnly, "readonly")
	add(flagAdmin, "admin")
	add(flagPubSub, "pubsub")
	add(flagNoScript, "noscript")
	add(flagNoAuth, "no_auth")
	add(flagFast, "fast")
	if out == nil {
		out = []string{}
	}
	return out
}

func upper(s string) string { return strings.ToUpper(s) }

func init() {
	registerKeyCommands()
	registerStringCommands()
	registerListCommands()
	registerSetCommands()
	registerHashCommands()
	registerServerCommands()
	registerPubSubCommands()
	registerScriptCommands()
	registerTxCommands()
}
