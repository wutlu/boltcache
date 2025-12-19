package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	// "log"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
 )

import (
	bcLogger "boltcache/logger"
)

type DataType int
	"unsafe"

	"github.com/panjf2000/gnet/v2"
	"github.com/tidwall/redcon"
)

// CacheItem represents an item in the cache
type CacheItem struct {
	Value     interface{}
	ExpiresAt time.Time
}

// BoltCache is the main cache structure
type BoltCache struct {
	data        *ShardedMap
	subscribers sync.Map
	mu          sync.RWMutex
	persistFile string
	replicas    []string
	luaEngine   *LuaEngine
	config      *Config
}

// NewBoltCache creates a new BoltCache instance
func NewBoltCache(persistFile ...string) *BoltCache {
	bc := &BoltCache{
		data: NewShardedMap(),
	}
	if len(persistFile) > 0 {
		bc.persistFile = persistFile[0]
	}
	return bc
}

// --- ShardedMap Implementation ---

const ShardCount = 2048

type Shard struct {
	mu    sync.RWMutex
	items map[string]interface{}
}

type ShardedMap struct {
	shards []*Shard
}

func NewShardedMap() *ShardedMap {
	sm := &ShardedMap{
		shards: make([]*Shard, ShardCount),
	}
	for i := 0; i < ShardCount; i++ {
		sm.shards[i] = &Shard{
			items: make(map[string]interface{}, 1024),
		}
	}
	return sm
}

func (sm *ShardedMap) getShard(key string) *Shard {
	var h uint32 = 2166136261
	for i := 0; i < len(key); i++ {
		h ^= uint32(key[i])
		h *= 16777619
	}
	return sm.shards[h&(ShardCount-1)]
}

func (sm *ShardedMap) Load(key string) (interface{}, bool) {
	shard := sm.getShard(key)
	shard.mu.RLock()
	val, ok := shard.items[key]
	shard.mu.RUnlock()
	return val, ok
}

func (sm *ShardedMap) Store(key string, value interface{}) {
	shard := sm.getShard(key)
	shard.mu.Lock()
	shard.items[key] = value
	shard.mu.Unlock()
}

func (sm *ShardedMap) Delete(key string) {
	shard := sm.getShard(key)
	shard.mu.Lock()
	delete(shard.items, key)
	shard.mu.Unlock()
}

func (sm *ShardedMap) Range(f func(key, value interface{}) bool) {
	for _, shard := range sm.shards {
		shard.mu.RLock()
		for k, v := range shard.items {
			if !f(k, v) {
				shard.mu.RUnlock()
				return
			}
		}
		shard.mu.RUnlock()
	}
}

// --- BoltCache Methods ---

func (c *BoltCache) Set(key string, value interface{}, ttl time.Duration) {
	var expiresAt time.Time
	if ttl > 0 {
		expiresAt = time.Now().Add(ttl)
	}

	item := &CacheItem{
		Value:     value,
		ExpiresAt: expiresAt,
	}

	c.data.Store(key, item)
}

func (c *BoltCache) Get(key string) (interface{}, bool) {
	val, ok := c.data.Load(key)
	if !ok {
		return nil, false
	}

	item := val.(*CacheItem)
	if !item.ExpiresAt.IsZero() && time.Now().After(item.ExpiresAt) {
		c.data.Delete(key)
		return nil, false
	}

	return item.Value, true
}

func (c *BoltCache) Delete(key string) {
	c.data.Delete(key)
}

func (c *BoltCache) AddReplica(addr string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.replicas = append(c.replicas, addr)
}

// List operations
func (c *BoltCache) LPush(key string, values ...string) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	var list []string
	if val, ok := c.Get(key); ok {
		if l, ok := val.([]string); ok {
			list = l
		}
	}

	list = append(values, list...)
	c.Set(key, list, 0)
	return len(list)
}

func (c *BoltCache) LPop(key string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if val, ok := c.Get(key); ok {
		if list, ok := val.([]string); ok && len(list) > 0 {
			item := list[0]
			list = list[1:]
			if len(list) == 0 {
				c.Delete(key)
			} else {
				c.Set(key, list, 0)
			}
			return item, true
		}
	}
	return "", false
}

// Set operations
func (c *BoltCache) SAdd(key string, members ...string) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	set := make(map[string]struct{})
	if val, ok := c.Get(key); ok {
		if s, ok := val.(map[string]struct{}); ok {
			set = s
		}
	}

	added := 0
	for _, m := range members {
		if _, ok := set[m]; !ok {
			set[m] = struct{}{}
			added++
		}
	}

	c.Set(key, set, 0)
	return len(set)
}

func (c *BoltCache) SMembers(key string) []string {
	if val, ok := c.Get(key); ok {
		if set, ok := val.(map[string]struct{}); ok {
			members := make([]string, 0, len(set))
			for m := range set {
				members = append(members, m)
			}
			return members
		}
	}
	return nil
}

// Hash operations
func (c *BoltCache) HSet(key, field, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	hash := make(map[string]string)
	if val, ok := c.Get(key); ok {
		if h, ok := val.(map[string]string); ok {
			hash = h
		}
	}

	hash[field] = value
	c.Set(key, hash, 0)
}

func (c *BoltCache) HGet(key, field string) (string, bool) {
	if val, ok := c.Get(key); ok {
		if hash, ok := val.(map[string]string); ok {
			v, ok := hash[field]
			return v, ok
		}
	}
	return "", false
}

// Pub/Sub
func (c *BoltCache) Subscribe(channel string, conn net.Conn) {
	conns, _ := c.subscribers.LoadOrStore(channel, &sync.Map{})
	conns.(*sync.Map).Store(conn, true)
}

func (c *BoltCache) Publish(channel string, message string) int {
	count := 0
	if conns, ok := c.subscribers.Load(channel); ok {
		conns.(*sync.Map).Range(func(key, value interface{}) bool {
			conn := key.(net.Conn)
			fmt.Fprintf(conn, "MESSAGE %s %s\n", channel, message)
			count++
			return true
		})
	}
	return count
}

// Persistence
func (c *BoltCache) loadFromDisk() {
	if c.persistFile == "" {
		return
	}

	data, err := os.ReadFile(c.persistFile)
	if err != nil {
		return
	}

func (c *BoltCache) cleanupExpired() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	
	for range ticker.C {
		now := time.Now()
		expiredKeys := make([]interface{}, 0)
		c.data.Range(func(key, value interface{}) bool {
			item := value.(*CacheItem)
			if !item.ExpiresAt.IsZero() && now.After(item.ExpiresAt) {
				expiredKeys = append(expiredKeys, key)
			}
			return true
		})
		
		for _, key := range expiredKeys {
			c.data.Delete(key)
		}
		
		if len(expiredKeys) > 0 {
			bcLogger.Log("Cleaned up %d expired keys", len(expiredKeys))
		}
	var items map[string]*CacheItem
	if err := json.Unmarshal(data, &items); err != nil {
		bcLogger.Log("Failed to unmarshal persistence data: %v", err)
		return
	}

	for k, v := range items {
		c.data.Store(k, v)
	}
	log.Printf("Loaded %d items from disk", len(items))
}

// Optimized handleConnection for standard TCP
func (c *BoltCache) handleConnection(conn net.Conn) {
	defer conn.Close()

	if tc, ok := conn.(*net.TCPConn); ok {
		tc.SetNoDelay(true)
	}

	reader := bufio.NewReaderSize(conn, 65536)

	for {
		line, err := reader.ReadSlice('\n')
		if err != nil {
			return
		}

		n := len(line)
		if n > 0 && line[n-1] == '\n' {
			n--
		}
		if n > 0 && line[n-1] == '\r' {
			n--
		}
		line = line[:n]

		if n < 4 {
			continue
		}

		parts := strings.Fields(string(line))
		if len(parts) == 0 {
			continue
		}

		cmd := strings.ToUpper(parts[0])

		switch cmd {
		case "SET":
			if len(parts) >= 3 {
				key, value := parts[1], parts[2]
				var ttl time.Duration
				if len(parts) >= 4 {
					if d, err := time.ParseDuration(parts[3]); err == nil {
						ttl = d
					}
				}
				c.Set(key, value, ttl)
				fmt.Fprintln(conn, "OK")
			} else {
				fmt.Fprintln(conn, "ERROR: SET key value [ttl]")
			}
		case "GET":
			if len(parts) >= 2 {
				key := parts[1]
				if value, ok := c.Get(key); ok {
					fmt.Fprintf(conn, "VALUE %v\n", value)
				} else {
					fmt.Fprintln(conn, "NIL")
				}
			} else {
				fmt.Fprintln(conn, "ERROR: GET key")
			}
		case "DEL":
			if len(parts) >= 2 {
				key := parts[1]
				c.Delete(key)
				fmt.Fprintln(conn, "OK")
			}
		case "PING":
			fmt.Fprintln(conn, "PONG")
		case "LPUSH":
			if len(parts) >= 3 {
				count := c.LPush(parts[1], parts[2:]...)
				fmt.Fprintf(conn, "INTEGER %d\n", count)
			}
		case "LPOP":
			if len(parts) >= 2 {
				if val, ok := c.LPop(parts[1]); ok {
					fmt.Fprintf(conn, "VALUE %s\n", val)
				} else {
					fmt.Fprintln(conn, "NIL")
				}
			}
		case "SADD":
			if len(parts) >= 3 {
				count := c.SAdd(parts[1], parts[2:]...)
				fmt.Fprintf(conn, "INTEGER %d\n", count)
			}
		case "SMEMBERS":
			if len(parts) >= 2 {
				members := c.SMembers(parts[1])
				fmt.Fprintf(conn, "ARRAY %s\n", strings.Join(members, " "))
			}
		case "HSET":
			if len(parts) >= 4 {
				c.HSet(parts[1], parts[2], parts[3])
				fmt.Fprintln(conn, "OK")
			}
		case "HGET":
			if len(parts) >= 3 {
				if val, ok := c.HGet(parts[1], parts[2]); ok {
					fmt.Fprintf(conn, "VALUE %s\n", val)
				} else {
					fmt.Fprintln(conn, "NIL")
				}
			}
		case "SUBSCRIBE":
			if len(parts) >= 2 {
				c.Subscribe(parts[1], conn)
				fmt.Fprintf(conn, "SUBSCRIBED %s\n", parts[1])
			}
		case "PUBLISH":
			if len(parts) >= 3 {
				count := c.Publish(parts[1], strings.Join(parts[2:], " "))
				fmt.Fprintf(conn, "PUBLISHED %d\n", count)
			}
		case "EVAL":
			if len(parts) >= 4 && c.luaEngine != nil {
				script := parts[1]
				numKeys, _ := strconv.Atoi(parts[2])
				keys := parts[3 : 3+numKeys]
				args := parts[3+numKeys:]
				result := c.luaEngine.Execute(script, keys, args)
				fmt.Fprintf(conn, "RESULT %v\n", result)
			}
		default:
			fmt.Fprintln(conn, "ERROR: Unknown command")
		}
	}
}

// --- gnet Server Implementation ---

type gnetServer struct {
	gnet.BuiltinEventEngine
	cache *BoltCache
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

func StartGnetServer(cache *BoltCache) {
	port := 6381 // Default gnet port
	if cache.config != nil && cache.config.Server.TCP.Port > 0 {
		port = cache.config.Server.TCP.Port + 1
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

// StartRESPServer starts a Redis-compatible RESP protocol server
func StartRESPServer(cache *BoltCache) {
	port := 6382 // RESP server port
	if cache.config != nil && cache.config.Server.TCP.Port > 0 {
		port = cache.config.Server.TCP.Port + 2
	}

	go func() {
		log.Printf("Starting RESP server (Redis-compatible) on port %d", port)

		addr := fmt.Sprintf(":%d", port)
		err := redcon.ListenAndServe(addr,
			func(conn redcon.Conn, cmd redcon.Command) {
				if len(cmd.Args) == 0 {
					conn.WriteError("ERR empty command")
					return
				}

				command := strings.ToLower(string(cmd.Args[0]))

				switch command {
				case "ping":
					conn.WriteString("PONG")

				case "set":
					if len(cmd.Args) < 3 {
						conn.WriteError("ERR wrong number of arguments for 'set' command")
						return
					}
					key := string(cmd.Args[1])
					value := cmd.Args[2]
					cache.Set(key, value, 0)
					conn.WriteString("OK")

				case "get":
					if len(cmd.Args) < 2 {
						conn.WriteError("ERR wrong number of arguments for 'get' command")
						return
					}
					key := string(cmd.Args[1])
					if val, ok := cache.Get(key); ok {
						switch v := val.(type) {
						case []byte:
							conn.WriteBulk(v)
						case string:
							conn.WriteBulkString(v)
						default:
							conn.WriteBulkString(fmt.Sprintf("%v", v))
						}
					} else {
						conn.WriteNull()
					}

				case "del":
					if len(cmd.Args) < 2 {
						conn.WriteError("ERR wrong number of arguments for 'del' command")
						return
					}
					key := string(cmd.Args[1])
					cache.Delete(key)
					conn.WriteInt(1)

				case "exists":
					if len(cmd.Args) < 2 {
						conn.WriteError("ERR wrong number of arguments for 'exists' command")
						return
					}
					key := string(cmd.Args[1])
					if _, ok := cache.Get(key); ok {
						conn.WriteInt(1)
					} else {
						conn.WriteInt(0)
					}

				case "lpush":
					if len(cmd.Args) < 3 {
						conn.WriteError("ERR wrong number of arguments for 'lpush' command")
						return
					}
					key := string(cmd.Args[1])
					values := make([]string, len(cmd.Args)-2)
					for i, arg := range cmd.Args[2:] {
						values[i] = string(arg)
					}
					count := cache.LPush(key, values...)
					conn.WriteInt(count)

				case "lpop":
					if len(cmd.Args) < 2 {
						conn.WriteError("ERR wrong number of arguments for 'lpop' command")
						return
					}
					key := string(cmd.Args[1])
					if val, ok := cache.LPop(key); ok {
						conn.WriteBulkString(val)
					} else {
						conn.WriteNull()
					}

				case "sadd":
					if len(cmd.Args) < 3 {
						conn.WriteError("ERR wrong number of arguments for 'sadd' command")
						return
					}
					key := string(cmd.Args[1])
					members := make([]string, len(cmd.Args)-2)
					for i, arg := range cmd.Args[2:] {
						members[i] = string(arg)
					}
					count := cache.SAdd(key, members...)
					conn.WriteInt(count)

				case "smembers":
					if len(cmd.Args) < 2 {
						conn.WriteError("ERR wrong number of arguments for 'smembers' command")
						return
					}
					key := string(cmd.Args[1])
					members := cache.SMembers(key)
					conn.WriteArray(len(members))
					for _, m := range members {
						conn.WriteBulkString(m)
					}

				case "hset":
					if len(cmd.Args) < 4 {
						conn.WriteError("ERR wrong number of arguments for 'hset' command")
						return
					}
					key := string(cmd.Args[1])
					field := string(cmd.Args[2])
					value := string(cmd.Args[3])
					cache.HSet(key, field, value)
					conn.WriteInt(1)

				case "hget":
					if len(cmd.Args) < 3 {
						conn.WriteError("ERR wrong number of arguments for 'hget' command")
						return
					}
					key := string(cmd.Args[1])
					field := string(cmd.Args[2])
					if val, ok := cache.HGet(key, field); ok {
						conn.WriteBulkString(val)
					} else {
						conn.WriteNull()
					}

				default:
					conn.WriteError("ERR unknown command '" + command + "'")
				}
			},
			func(conn redcon.Conn) bool {
				// Accept connection
				return true
			},
			func(conn redcon.Conn, err error) {
				// Connection closed
			},
		)

		if err != nil {
			log.Printf("RESP server error: %v", err)
		}
	}()
}
