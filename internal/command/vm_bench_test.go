package command

import "testing"

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
