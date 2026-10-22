package command

import (
	"fmt"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/wutlu/boltcache/internal/cache"
	"github.com/wutlu/boltcache/internal/resp"
)

func registerServerCommands() {
	register("ping", cmdPing, -1, flagNoAuth|flagPubSub|flagFast, 0, 0, 0)
	register("echo", cmdEcho, 2, flagFast, 0, 0, 0)
	register("auth", cmdAuth, -2, flagNoAuth|flagNoScript|flagPubSub|flagFast, 0, 0, 0)
	register("hello", cmdHello, -1, flagNoAuth|flagNoScript|flagFast, 0, 0, 0)
	register("quit", cmdQuit, -1, flagNoAuth|flagNoScript|flagPubSub|flagFast|flagMultiControl, 0, 0, 0)
	register("reset", cmdReset, 1, flagNoAuth|flagNoScript|flagPubSub|flagFast|flagMultiControl, 0, 0, 0)
	register("select", cmdSelect, 2, flagFast, 0, 0, 0)
	register("time", cmdTime, 1, flagFast, 0, 0, 0)
	register("info", cmdInfo, -1, flagNoScript|flagNoTx, 0, 0, 0)
	register("config", cmdConfig, -2, flagAdmin|flagNoScript|flagNoTx, 0, 0, 0)
	register("client", cmdClient, -2, flagNoScript|flagNoTx, 0, 0, 0)
	register("command", cmdCommand, -1, flagNoTx, 0, 0, 0)
	register("save", cmdSave, 1, flagAdmin|flagNoScript|flagNoTx, 0, 0, 0)
	register("bgsave", cmdSave, -1, flagAdmin|flagNoScript|flagNoTx, 0, 0, 0)
	register("lastsave", cmdLastSave, 1, flagFast, 0, 0, 0)
	register("wait", cmdWait, 3, flagNoScript, 0, 0, 0)
	register("memory", cmdMemory, -2, flagReadOnly, 0, 0, 0)
}

func cmdPing(_ *Engine, s *Session, _ *cache.Tx, args []string, w resp.Writer) {
	if len(args) > 2 {
		w.Error("ERR wrong number of arguments for 'ping' command")
		return
	}
	if s.Subscriptions() > 0 {
		w.Array(2)
		w.Bulk("pong")
		if len(args) == 2 {
			w.Bulk(args[1])
		} else {
			w.Bulk("")
		}
		return
	}
	if len(args) == 2 {
		w.Bulk(args[1])
		return
	}
	w.Simple("PONG")
}

func cmdEcho(_ *Engine, _ *Session, _ *cache.Tx, args []string, w resp.Writer) { w.Bulk(args[1]) }

func cmdAuth(e *Engine, s *Session, _ *cache.Tx, args []string, w resp.Writer) {
	if len(args) > 3 {
		w.Error(errSyntax)
		return
	}
	if !e.AuthRequired() {
		w.Error("ERR AUTH <password> called without any password configured for the default user. Are you sure your configuration is correct?")
		return
	}
	// AUTH <password> or AUTH <username> <password>; usernames other than
	// "default" don't exist.
	pass := args[len(args)-1]
	if (len(args) == 3 && args[1] != "default") || !e.CheckPassword(pass) {
		w.Error("WRONGPASS invalid username-password pair or user is disabled.")
		return
	}
	s.Authenticated = true
	w.Simple("OK")
}

func cmdHello(e *Engine, s *Session, _ *cache.Tx, args []string, w resp.Writer) {
	if len(args) >= 2 {
		v, ok := parseInt(args[1])
		if !ok {
			w.Error("ERR Protocol version is not an integer or out of range")
			return
		}
		if v != 2 {
			// RESP3 isn't implemented; clients fall back to RESP2.
			w.Error("NOPROTO unsupported protocol version")
			return
		}
		for i := 2; i < len(args); i++ {
			switch upper(args[i]) {
			case "AUTH":
				if i+2 >= len(args) {
					w.Error(errSyntax)
					return
				}
				if e.AuthRequired() && (args[i+1] != "default" || !e.CheckPassword(args[i+2])) {
					w.Error("WRONGPASS invalid username-password pair or user is disabled.")
					return
				}
				s.Authenticated = true
				i += 2
			case "SETNAME":
				if i+1 >= len(args) {
					w.Error(errSyntax)
					return
				}
				s.Name = args[i+1]
				i++
			default:
				w.Error(errSyntax)
				return
			}
		}
	}
	if !s.Authenticated {
		w.Error("NOAUTH HELLO must be called with the client already authenticated, otherwise the HELLO <proto> AUTH <user> <pass> option can be used to authenticate the client and select the RESP protocol version at the same time")
		return
	}
	w.Array(14)
	w.Bulk("server")
	w.Bulk("redis")
	w.Bulk("version")
	w.Bulk(redisCompatVersion)
	w.Bulk("proto")
	w.Int(2)
	w.Bulk("id")
	w.Int(s.ID)
	w.Bulk("mode")
	w.Bulk("standalone")
	w.Bulk("role")
	w.Bulk("master")
	w.Bulk("modules")
	w.Array(0)
}

// redisCompatVersion is the Redis version whose command set BoltCache
// follows; some clients use it for feature detection.
const redisCompatVersion = "7.2.0"

func cmdQuit(_ *Engine, s *Session, _ *cache.Tx, _ []string, w resp.Writer) {
	s.Close = true
	w.Simple("OK")
}

func cmdReset(e *Engine, s *Session, _ *cache.Tx, _ []string, w resp.Writer) {
	e.CloseSession(s)
	s.inMulti, s.multiAborted, s.queue, s.watched = false, false, nil, nil
	s.Name = ""
	s.Authenticated = !e.AuthRequired()
	w.Simple("RESET")
}

func cmdSelect(_ *Engine, _ *Session, _ *cache.Tx, args []string, w resp.Writer) {
	n, ok := parseInt(args[1])
	if !ok {
		w.Error(errNotInt)
		return
	}
	if n != 0 {
		w.Error("ERR DB index is out of range")
		return
	}
	w.Simple("OK")
}

func cmdTime(_ *Engine, _ *Session, _ *cache.Tx, _ []string, w resp.Writer) {
	now := time.Now()
	w.Array(2)
	w.Bulk(strconv.FormatInt(now.Unix(), 10))
	w.Bulk(strconv.FormatInt(int64(now.Nanosecond()/1000), 10))
}

func cmdLastSave(e *Engine, _ *Session, _ *cache.Tx, _ []string, w resp.Writer) {
	var t time.Time
	if e.Persister != nil {
		t = e.Persister.LastSave()
	}
	if t.IsZero() {
		t = e.started
	}
	w.Int(t.Unix())
}

func cmdSave(e *Engine, _ *Session, _ *cache.Tx, args []string, w resp.Writer) {
	if e.Persister == nil {
		w.Error("ERR persistence is disabled (persistence.enabled)")
		return
	}
	if lowerASCII(args[0]) == "bgsave" {
		go func() { _, _ = e.Persister.Save(true) }()
		w.Simple("Background saving started")
		return
	}
	if _, err := e.Persister.Save(true); err != nil {
		w.Error("ERR " + err.Error())
		return
	}
	w.Simple("OK")
}

func cmdWait(_ *Engine, _ *Session, _ *cache.Tx, _ []string, w resp.Writer) { w.Int(0) }

func cmdMemory(e *Engine, _ *Session, db *cache.Tx, args []string, w resp.Writer) {
	switch upper(args[1]) {
	case "USAGE":
		if len(args) < 3 {
			w.Error(errSyntax)
			return
		}
		n, err := db.MemoryUsage(args[2])
		if err != nil {
			writeErr(w, err)
			return
		}
		if n == 0 {
			w.Null()
			return
		}
		w.Int(n)
	case "STATS":
		w.Array(4)
		w.Bulk("dataset.bytes")
		w.Int(e.cache.UsedMemory())
		w.Bulk("keys.count")
		w.Int(e.cache.Len())
	default:
		w.Error("ERR unknown subcommand '" + args[1] + "'. Try MEMORY USAGE or MEMORY STATS.")
	}
}

func cmdConfig(e *Engine, _ *Session, _ *cache.Tx, args []string, w resp.Writer) {
	switch upper(args[1]) {
	case "GET":
		if len(args) < 3 {
			w.Error("ERR wrong number of arguments for 'config|get' command")
			return
		}
		params := e.configParams()
		var out []string
		names := make([]string, 0, len(params))
		for n := range params {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			for _, pat := range args[2:] {
				if globMatchFold(pat, n) {
					out = append(out, n, params[n])
					break
				}
			}
		}
		if out == nil {
			out = []string{}
		}
		writeStrings(w, out)
	case "SET":
		w.Error("ERR CONFIG SET is not supported; edit the configuration file and restart")
	case "RESETSTAT":
		w.Simple("OK")
	default:
		w.Error("ERR unknown subcommand '" + args[1] + "'. Try CONFIG GET.")
	}
}

func (e *Engine) configParams() map[string]string {
	o := e.cache.Options()
	policy := string(o.EvictionPolicy)
	if policy != string(cache.PolicyNoEviction) {
		policy = "allkeys-" + policy
	}
	save, appendonly := "", "no"
	if e.Persister != nil {
		save = "periodic"
	}
	return map[string]string{
		"maxmemory":        strconv.FormatInt(o.MaxMemory, 10),
		"maxmemory-policy": policy,
		"databases":        "1",
		"save":             save,
		"appendonly":       appendonly,
		"maxclients":       "10000",
	}
}

func globMatchFold(pattern, s string) bool {
	return pattern == "*" || strings.EqualFold(pattern, s) ||
		(strings.HasSuffix(pattern, "*") && strings.HasPrefix(strings.ToLower(s), strings.ToLower(strings.TrimSuffix(pattern, "*"))))
}

func cmdClient(e *Engine, s *Session, _ *cache.Tx, args []string, w resp.Writer) {
	switch upper(args[1]) {
	case "ID":
		w.Int(s.ID)
	case "SETNAME":
		if len(args) != 3 {
			w.Error(errSyntax)
			return
		}
		if strings.ContainsAny(args[2], " \n") {
			w.Error("ERR Client names cannot contain spaces, newlines or special characters.")
			return
		}
		s.Name = args[2]
		w.Simple("OK")
	case "GETNAME":
		writeOptional(w, s.Name, s.Name != "")
	case "SETINFO", "NO-EVICT", "NO-TOUCH", "TRACKING", "REPLY":
		w.Simple("OK")
	case "INFO", "LIST":
		w.Bulk(fmt.Sprintf("id=%d addr=%s name=%s age=%d db=0 sub=%d psub=%d multi=%d cmd=client\n",
			s.ID, s.Addr, s.Name, int(time.Since(s.created).Seconds()), len(s.channels), len(s.patterns), boolInt(s.inMulti)*len(s.queue)))
	default:
		w.Error("ERR unknown subcommand '" + args[1] + "'. Try CLIENT HELP.")
	}
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func cmdCommand(_ *Engine, _ *Session, _ *cache.Tx, args []string, w resp.Writer) {
	if len(args) == 1 {
		names := Names()
		w.Array(len(names))
		for _, n := range names {
			writeCommandInfo(w, table[n])
		}
		return
	}
	switch upper(args[1]) {
	case "COUNT":
		w.Int(int64(len(table)))
	case "LIST":
		writeStrings(w, Names())
	case "INFO":
		w.Array(len(args) - 2)
		for _, n := range args[2:] {
			if sp := lookup(n); sp != nil {
				writeCommandInfo(w, sp)
			} else {
				w.NullArray()
			}
		}
	case "DOCS":
		w.Array(0)
	case "GETKEYS":
		if len(args) < 3 {
			w.Error(errSyntax)
			return
		}
		sp := lookup(args[2])
		if sp == nil {
			w.Error("ERR Invalid command specified")
			return
		}
		writeStrings(w, sp.keys(args[2:]))
	default:
		w.Error("ERR unknown subcommand '" + args[1] + "'. Try COMMAND HELP.")
	}
}

func writeCommandInfo(w resp.Writer, sp *spec) {
	w.Array(6)
	w.Bulk(sp.name)
	w.Int(int64(sp.arity))
	writeStatusList(w, sp.flagNames())
	w.Int(int64(sp.first))
	w.Int(int64(sp.last))
	w.Int(int64(sp.step))
}

func writeStatusList(w resp.Writer, vals []string) {
	w.Array(len(vals))
	for _, v := range vals {
		w.Simple(v)
	}
}

// Info renders the INFO text for the given section ("" or "all" for every
// section).
func (e *Engine) Info(section string) string {
	section = strings.ToLower(section)
	all := section == "" || section == "all" || section == "everything" || section == "default"
	var b strings.Builder
	write := func(name string, lines ...string) {
		if !all && section != name {
			return
		}
		if b.Len() > 0 {
			b.WriteString("\r\n")
		}
		b.WriteString("# " + strings.ToUpper(name[:1]) + name[1:] + "\r\n")
		for _, l := range lines {
			b.WriteString(l + "\r\n")
		}
	}
	uptime := time.Since(e.started)
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	st := e.cache.Stats()
	o := e.cache.Options()
	var clients int64
	if e.ClientCount != nil {
		clients = e.ClientCount()
	}
	commands, rejected := e.Stats()

	write("server",
		"redis_version:"+redisCompatVersion,
		"boltcache_version:"+e.cfg.Version,
		"redis_mode:standalone",
		"os:"+runtime.GOOS+" "+runtime.GOARCH,
		"arch_bits:64",
		"go_version:"+runtime.Version(),
		"process_id:"+strconv.Itoa(os.Getpid()),
		"uptime_in_seconds:"+strconv.FormatInt(int64(uptime.Seconds()), 10),
		"uptime_in_days:"+strconv.FormatInt(int64(uptime.Hours()/24), 10),
	)
	write("clients",
		"connected_clients:"+strconv.FormatInt(clients, 10),
		"pubsub_patterns:"+strconv.Itoa(e.hub.NumPat()),
		"pubsub_channels:"+strconv.Itoa(len(e.hub.Channels(""))),
	)
	write("memory",
		"used_memory:"+strconv.FormatInt(st.UsedMemory, 10),
		"used_memory_human:"+humanBytes(st.UsedMemory),
		"used_memory_rss:"+strconv.FormatUint(ms.Sys, 10),
		"go_heap_alloc:"+strconv.FormatUint(ms.HeapAlloc, 10),
		"maxmemory:"+strconv.FormatInt(o.MaxMemory, 10),
		"maxmemory_human:"+humanBytes(o.MaxMemory),
		"maxmemory_policy:"+string(o.EvictionPolicy),
		"maxkeys:"+strconv.FormatInt(o.MaxKeys, 10),
	)
	var lastSave time.Time
	if e.Persister != nil {
		lastSave = e.Persister.LastSave()
	}
	write("persistence",
		"loading:0",
		"persistence_enabled:"+strconv.Itoa(boolInt(e.Persister != nil)),
		"rdb_last_save_time:"+strconv.FormatInt(lastSave.Unix(), 10),
	)
	write("stats",
		"total_commands_processed:"+strconv.FormatUint(commands, 10),
		"rejected_commands:"+strconv.FormatUint(rejected, 10),
		"keyspace_hits:"+strconv.FormatUint(st.Hits, 10),
		"keyspace_misses:"+strconv.FormatUint(st.Misses, 10),
		"expired_keys:"+strconv.FormatUint(st.Expired, 10),
		"evicted_keys:"+strconv.FormatUint(st.Evicted, 10),
	)
	write("replication", "role:master", "connected_slaves:0")
	write("cpu", "go_goroutines:"+strconv.Itoa(runtime.NumGoroutine()), "gomaxprocs:"+strconv.Itoa(runtime.GOMAXPROCS(0)))
	keyspace := []string{}
	if st.Keys > 0 {
		keyspace = append(keyspace, fmt.Sprintf("db0:keys=%d,expires=%d,avg_ttl=0", st.Keys, st.KeysWithTTL))
	}
	write("keyspace", keyspace...)
	return b.String()
}

func cmdInfo(e *Engine, _ *Session, _ *cache.Tx, args []string, w resp.Writer) {
	section := ""
	if len(args) > 1 {
		section = args[1]
	}
	w.Bulk(e.Info(section))
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return strconv.FormatInt(n, 10) + "B"
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f%c", float64(n)/float64(div), "KMGTPE"[exp])
}
