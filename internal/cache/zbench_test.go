package cache

import (
	"math/rand/v2"
	"strconv"
	"testing"
)

// Mirrors redis-benchmark's ZADD test: random scores and members on one key.
func BenchmarkZAddRandom(b *testing.B) {
	c := New(Options{})
	defer c.Close()
	members := make([]string, 100000)
	for i := range members {
		members[i] = "element:" + strconv.Itoa(i)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = c.ZAdd("myzset", ZAddOptions{}, []ZMember{{members[rand.IntN(len(members))], float64(rand.IntN(100000))}})
	}
}
