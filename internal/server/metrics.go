package server

import (
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"github.com/wutlu/boltcache/v2/internal/cache"
	"github.com/wutlu/boltcache/v2/internal/command"
)

// metrics renders Prometheus text exposition format. It's hand-written to
// avoid pulling the Prometheus client library into a small binary.
type metrics struct {
	started   time.Time
	version   string
	engine    *command.Engine
	persister func() *cache.Persister
	clients   *atomic.Int64

	http2xx, http3xx, http4xx, http5xx atomic.Uint64
}

func (m *metrics) uptime() time.Duration { return time.Since(m.started) }

func (m *metrics) observeHTTP(status int) {
	switch {
	case status >= 500:
		m.http5xx.Add(1)
	case status >= 400:
		m.http4xx.Add(1)
	case status >= 300:
		m.http3xx.Add(1)
	default:
		m.http2xx.Add(1)
	}
}

func (m *metrics) handler(w http.ResponseWriter, _ *http.Request) {
	var b strings.Builder
	metric := func(name, typ, help string, value any, labels ...string) {
		fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, typ)
		if len(labels) > 0 {
			fmt.Fprintf(&b, "%s{%s} %v\n", name, strings.Join(labels, ","), value)
		} else {
			fmt.Fprintf(&b, "%s %v\n", name, value)
		}
	}

	c := m.engine.Cache()
	st := c.Stats()
	o := c.Options()
	commands, rejected := m.engine.Stats()
	hub := m.engine.Hub()

	metric("boltcache_build_info", "gauge", "Build information.", 1,
		fmt.Sprintf(`version=%q`, m.version), fmt.Sprintf(`goversion=%q`, runtime.Version()))
	metric("boltcache_uptime_seconds", "gauge", "Seconds since the server started.", int64(m.uptime().Seconds()))
	metric("boltcache_keys", "gauge", "Number of keys.", st.Keys)
	metric("boltcache_keys_with_ttl", "gauge", "Number of keys with an expiration.", st.KeysWithTTL)
	metric("boltcache_memory_used_bytes", "gauge", "Approximate memory used by keys and values.", st.UsedMemory)
	metric("boltcache_memory_max_bytes", "gauge", "Configured memory limit (0 = unlimited).", o.MaxMemory)
	metric("boltcache_keys_max", "gauge", "Configured key limit (0 = unlimited).", o.MaxKeys)
	metric("boltcache_keyspace_hits_total", "counter", "Successful key lookups.", st.Hits)
	metric("boltcache_keyspace_misses_total", "counter", "Failed key lookups.", st.Misses)
	metric("boltcache_expired_keys_total", "counter", "Keys removed because they expired.", st.Expired)
	metric("boltcache_evicted_keys_total", "counter", "Keys evicted because a limit was reached.", st.Evicted)
	metric("boltcache_commands_processed_total", "counter", "Commands executed.", commands)
	metric("boltcache_commands_rejected_total", "counter", "Commands rejected (unknown, bad arity, auth...).", rejected)
	metric("boltcache_connected_clients", "gauge", "Open client connections (TCP, RESP, WebSocket).", m.clients.Load())
	metric("boltcache_pubsub_channels", "gauge", "Channels with at least one subscriber.", len(hub.Channels("")))
	metric("boltcache_pubsub_patterns", "gauge", "Pattern subscriptions.", hub.NumPat())
	if p := m.persister(); p != nil {
		var ts int64
		if t := p.LastSave(); !t.IsZero() {
			ts = t.Unix()
		}
		metric("boltcache_last_save_timestamp_seconds", "gauge", "Time of the last successful snapshot.", ts)
	}

	fmt.Fprintf(&b, "# HELP boltcache_http_responses_total HTTP responses by status class.\n# TYPE boltcache_http_responses_total counter\n")
	for _, x := range []struct {
		class string
		v     *atomic.Uint64
	}{{"2xx", &m.http2xx}, {"3xx", &m.http3xx}, {"4xx", &m.http4xx}, {"5xx", &m.http5xx}} {
		fmt.Fprintf(&b, "boltcache_http_responses_total{code=%q} %d\n", x.class, x.v.Load())
	}

	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	metric("go_goroutines", "gauge", "Number of goroutines.", runtime.NumGoroutine())
	metric("go_memstats_heap_alloc_bytes", "gauge", "Heap bytes allocated and in use.", ms.HeapAlloc)
	metric("go_memstats_sys_bytes", "gauge", "Bytes obtained from the OS.", ms.Sys)
	metric("go_gc_cycles_total", "counter", "Completed GC cycles.", ms.NumGC)

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, _ = w.Write([]byte(b.String()))
}
