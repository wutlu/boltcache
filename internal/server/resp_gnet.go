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

// RESP Protocol Parser for gnet
type respServer struct {
	gnet.BuiltinEventEngine
	cache *cache.BoltCache
}

var (
	respOKResp   = []byte("+OK\r\n")
	respNullResp = []byte("$-1\r\n")
	respPongResp = []byte("+PONG\r\n")
)

var respBatchPool = sync.Pool{
	New: func() interface{} {
		b := make([]byte, 0, 65536)
		return &b
	},
}

//go:nosplit
func b2sResp(b []byte) string {
	return unsafe.String(unsafe.SliceData(b), len(b))
}

func (rs *respServer) OnTraffic(c gnet.Conn) gnet.Action {
	data, _ := c.Next(-1)
	if len(data) == 0 {
		return gnet.None
	}

	batchPtr := respBatchPool.Get().(*[]byte)
	batch := *batchPtr
	batch = batch[:0]

	for len(data) > 0 {
		// Parse RESP array (*3\r\n$3\r\nSET\r\n...)
		if len(data) < 4 || data[0] != '*' {
			break
		}

		// Find array length
		idx := bytes.IndexByte(data, '\r')
		if idx == -1 {
			break
		}

		// Skip *N\r\n
		data = data[idx+2:]

		// Parse command arguments
		var args [][]byte
		for {
			if len(data) < 4 {
				break
			}

			if data[0] != '$' {
				break
			}

			// Find bulk string length
			idx := bytes.IndexByte(data, '\r')
			if idx == -1 {
				break
			}

			// Parse length
			lenStr := data[1:idx]
			argLen := 0
			for _, b := range lenStr {
				argLen = argLen*10 + int(b-'0')
			}

			// Skip $N\r\n
			data = data[idx+2:]

			if len(data) < argLen+2 {
				break
			}

			// Extract argument
			arg := data[:argLen]
			args = append(args, arg)

			// Skip arg\r\n
			data = data[argLen+2:]

			// Check if we have all args
			if len(args) >= 3 {
				break
			}
		}

		if len(args) == 0 {
			continue
		}

		// Execute command
		cmd := b2sResp(args[0])

		switch cmd {
		case "PING", "ping":
			batch = append(batch, respPongResp...)

		case "SET", "set":
			if len(args) >= 3 {
				key := string(args[1])
				val := make([]byte, len(args[2]))
				copy(val, args[2])
				rs.cache.Set(key, val, 0)
				batch = append(batch, respOKResp...)
			}

		case "GET", "get":
			if len(args) >= 2 {
				key := b2sResp(args[1])
				if val, ok := rs.cache.Get(key); ok {
					var valBytes []byte
					switch v := val.(type) {
					case []byte:
						valBytes = v
					case string:
						valBytes = []byte(v)
					default:
						valBytes = []byte(fmt.Sprintf("%v", v))
					}
					// $N\r\nVALUE\r\n
					batch = append(batch, '$')
					batch = append(batch, []byte(fmt.Sprintf("%d", len(valBytes)))...)
					batch = append(batch, '\r', '\n')
					batch = append(batch, valBytes...)
					batch = append(batch, '\r', '\n')
				} else {
					batch = append(batch, respNullResp...)
				}
			}
		}
	}

	if len(batch) > 0 {
		c.Write(batch)
	}
	*batchPtr = batch
	respBatchPool.Put(batchPtr)
	return gnet.None
}

func StartRESPGnetServer(cache *cache.BoltCache, cfg *config.Config) {
	port := 6382
	if cfg != nil && cfg.Server.TCP.Port > 0 {
		port = cfg.Server.TCP.Port + 2
	}

	rs := &respServer{cache: cache}
	go func() {
		log.Printf("Starting RESP gnet server (multicore) on port %d", port)
		err := gnet.Run(rs, fmt.Sprintf("tcp://:%d", port),
			gnet.WithMulticore(true),
			gnet.WithTCPNoDelay(gnet.TCPNoDelay),
			gnet.WithEdgeTriggeredIO(true),
			gnet.WithReusePort(true),
		)
		if err != nil {
			log.Printf("RESP gnet server error: %v", err)
		}
	}()
}
