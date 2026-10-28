package command

import (
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/wutlu/boltcache/v2/internal/resp"
)

func BenchmarkNewVM(b *testing.B) {
	for i := 0; i < b.N; i++ {
		v := newVM()
		v.L.Close()
	}
}

func BenchmarkEvalPooled(b *testing.B) {
	h := newHarness(b, Config{})
	for i := 0; i < b.N; i++ {
		h.e.ExecStrings(h.s, []string{"EVAL", "return redis.call('INCR', KEYS[1])", "1", "k"}, discard{})
	}
}

type discard struct{}

func (discard) Simple(string)    {}
func (discard) Error(string)     {}
func (discard) Int(int64)        {}
func (discard) Bulk(string)      {}
func (discard) BulkBytes([]byte) {}
func (discard) Null()            {}
func (discard) Array(int)        {}
func (discard) NullArray()       {}

// BenchmarkPipelineSetGet measures the engine's per-command cost (no
// network): 16-command pipelines alternating SET and GET on random keys.
func BenchmarkPipelineSetGet(b *testing.B) {
	h := newHarness(b, Config{})
	keys := make([]string, 100000)
	for i := range keys {
		keys[i] = "key:" + strconv.Itoa(i)
	}
	var enc resp.Encoder
	batch := make([][]string, 16)
	b.ResetTimer()
	for i := 0; i < b.N; i += 16 {
		for j := range batch {
			k := keys[(i+j*7919)%len(keys)]
			if j%2 == 0 {
				batch[j] = []string{"SET", k, "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"}
			} else {
				batch[j] = []string{"GET", k}
			}
		}
		enc.Buf = enc.Buf[:0]
		h.e.ExecPipeline(h.s, batch, &enc)
	}
}

// BenchmarkCommands measures the engine cost of individual commands in
// 16-command pipelines over 100k random keys (no network).
func BenchmarkCommands(b *testing.B) {
	keys := make([]string, 100000)
	for i := range keys {
		keys[i] = "key:" + strconv.Itoa(i)
	}
	val := "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
	cases := map[string]func(i int) []string{
		"GET":  func(i int) []string { return []string{"GET", keys[i%len(keys)]} },
		"SET":  func(i int) []string { return []string{"SET", keys[i%len(keys)], val} },
		"INCR": func(i int) []string { return []string{"INCR", keys[i%len(keys)]} },
		"MSET": func(i int) []string {
			a := []string{"MSET"}
			for j := 0; j < 10; j++ {
				a = append(a, keys[(i*10+j)%len(keys)], val)
			}
			return a
		},
		"ZADD": func(i int) []string {
			return []string{"ZADD", "myzset", strconv.Itoa(i % len(keys)), keys[i%len(keys)]}
		},
	}
	for _, name := range []string{"GET", "SET", "INCR", "MSET", "ZADD"} {
		b.Run(name, func(b *testing.B) {
			h := newHarness(b, Config{})
			gen := cases[name]
			// Prebuild commands so the benchmark measures the engine only.
			cmds := make([][]string, 1<<16)
			for i := range cmds {
				cmds[i] = gen(i * 7919)
			}
			if name != "MSET" && name != "ZADD" {
				for _, k := range keys {
					h.e.ExecStrings(h.s, []string{"SET", k, "1"}, discard{})
				}
			}
			var enc resp.Encoder
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i += 16 {
				off := i & (len(cmds) - 1)
				enc.Buf = enc.Buf[:0]
				h.e.ExecPipeline(h.s, cmds[off:off+16], &enc)
			}
		})
	}
}

// BenchmarkHotKeyParallel measures many sessions pipelining writes to one
// key (redis-benchmark's ZADD/HSET/SADD tests), where shard lock contention
// dominates.
func BenchmarkHotKeyParallel(b *testing.B) {
	for _, name := range []string{"ZADD", "HSET", "SADD"} {
		b.Run(name, func(b *testing.B) {
			h := newHarness(b, Config{})
			var seq atomic.Int64
			b.SetParallelism(1)
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				s := h.e.NewSession("bench", nil)
				batch := make([][]string, 16)
				var enc resp.Encoder
				for pb.Next() {
					base := seq.Add(16)
					for j := range batch {
						m := "element:" + strconv.FormatInt((base+int64(j))*7919%100000, 10)
						switch name {
						case "ZADD":
							batch[j] = []string{"ZADD", "myzset", strconv.FormatInt(base%1000, 10), m}
						case "HSET":
							batch[j] = []string{"HSET", "myhash", m, "xxx"}
						default:
							batch[j] = []string{"SADD", "myset", m}
						}
					}
					enc.Buf = enc.Buf[:0]
					h.e.ExecPipeline(s, batch, &enc)
				}
			})
			b.ReportMetric(float64(b.N*16)/b.Elapsed().Seconds()/1e6, "Mops/s")
		})
	}
}
