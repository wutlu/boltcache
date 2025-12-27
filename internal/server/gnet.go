package server

import (
	"bytes"
	"fmt"
	"log"
	"sync"
	"unsafe"

	"github.com/panjf2000/gnet/v2"
)

import (
	config "boltcache/config"
	cache "boltcache/internal/cache"
)

type gnetServer struct {
	gnet.BuiltinEventEngine
	cache *cache.BoltCache
}

var (
	respOK      = []byte("OK\n")
	respNIL     = []byte("NIL\n")
	valuePrefix = []byte("VALUE ")
	newLine     = []byte("\n")
)

var batchPool = sync.Pool{
	New: func() interface{} {
		b := make([]byte, 0, 65536)
		return &b
	},
}


//go:nosplit
func b2s(b []byte) string {
	return unsafe.String(unsafe.SliceData(b), len(b))
}

func (gs *gnetServer) OnTraffic(c gnet.Conn) gnet.Action {
	data, _ := c.Next(-1)
	if len(data) == 0 {
		return gnet.None
	}

	batchPtr := batchPool.Get().(*[]byte)
	batch := *batchPtr
	batch = batch[:0]

	for len(data) > 0 {
		idx := bytes.IndexByte(data, '\n')
		if idx == -1 {
			break
		}
		line := data[:idx]
		data = data[idx+1:]

		if len(line) > 0 && line[len(line)-1] == '\r' {
			line = line[:len(line)-1]
		}

		if len(line) < 4 {
			continue
		}

		cmd := line[0] | 0x20
		if cmd == 's' { // SET
			rest := line[4:]
			sp := bytes.IndexByte(rest, ' ')
			if sp == -1 {
				continue
			}
			key := string(rest[:sp])
			val := make([]byte, len(rest[sp+1:]))
			copy(val, rest[sp+1:])

			gs.cache.Set(key, val, 0)
			batch = append(batch, respOK...)
		} else if cmd == 'g' { // GET
			key := b2s(line[4:])
			val, ok := gs.cache.Get(key)
			if ok {
				batch = append(batch, valuePrefix...)
				switch v := val.(type) {
				case []byte:
					batch = append(batch, v...)
				case string:
					batch = append(batch, v...)
				default:
					batch = append(batch, []byte(fmt.Sprintf("%v", v))...)
				}
				batch = append(batch, newLine...)
			} else {
				batch = append(batch, respNIL...)
			}
		}
	}

	if len(batch) > 0 {
		c.Write(batch)
	}
	*batchPtr = batch
	batchPool.Put(batchPtr)
	return gnet.None
}

func StartGnetServer(cache *cache.BoltCache, cfg *config.Config) {
	port := 6381 // Default gnet port
	if cfg != nil && cfg.Server.TCP.Port > 0 {
		port = cfg.Server.TCP.Port + 1
	}

	gs := &gnetServer{cache: cache}
	go func() {
		log.Printf("Starting gnet server on port %d", port)
		err := gnet.Run(gs, fmt.Sprintf("tcp://:%d", port),
			gnet.WithMulticore(true),
			gnet.WithTCPNoDelay(gnet.TCPNoDelay),
			gnet.WithEdgeTriggeredIO(true),
		)
		if err != nil {
			log.Printf("gnet server error: %v", err)
		}
	}()
}
