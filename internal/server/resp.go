package server

import (
	"fmt"
	"github.com/tidwall/redcon"
	"log"
	"strings"
)

import (
	config "boltcache/config"
	cache "boltcache/internal/cache"
)

// StartRESPServer starts a Redis-compatible RESP protocol server
func StartRESPServer(cache *cache.BoltCache, cfg *config.Config) {
	port := 6382 // RESP server port
	if cfg != nil && cfg.Server.TCP.Port > 0 {
		port = cfg.Server.TCP.Port + 2
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
