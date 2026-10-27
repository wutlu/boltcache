package command

import (
	"strconv"
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
