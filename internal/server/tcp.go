package server

import (
	"bufio"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

import (
	_cache "boltcache/internal/cache"
)

// Optimized handleConnection for standard TCP
func handleConnection(conn net.Conn, cache *_cache.BoltCache) {
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
				cache.Set(key, value, ttl)
				fmt.Fprintln(conn, "OK")
			} else {
				fmt.Fprintln(conn, "ERROR: SET key value [ttl]")
			}
		case "GET":
			if len(parts) >= 2 {
				key := parts[1]
				if value, ok := cache.Get(key); ok {
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
				cache.Delete(key)
				fmt.Fprintln(conn, "OK")
			}
		case "PING":
			fmt.Fprintln(conn, "PONG")
		case "LPUSH":
			if len(parts) >= 3 {
				count := cache.LPush(parts[1], parts[2:]...)
				fmt.Fprintf(conn, "INTEGER %d\n", count)
			}
		case "LPOP":
			if len(parts) >= 2 {
				if val, ok := cache.LPop(parts[1]); ok {
					fmt.Fprintf(conn, "VALUE %s\n", val)
				} else {
					fmt.Fprintln(conn, "NIL")
				}
			}
		case "SADD":
			if len(parts) >= 3 {
				count := cache.SAdd(parts[1], parts[2:]...)
				fmt.Fprintf(conn, "INTEGER %d\n", count)
			}
		case "SMEMBERS":
			if len(parts) >= 2 {
				members := cache.SMembers(parts[1])
				fmt.Fprintf(conn, "ARRAY %s\n", strings.Join(members, " "))
			}
		case "HSET":
			if len(parts) >= 4 {
				cache.HSet(parts[1], parts[2], parts[3])
				fmt.Fprintln(conn, "OK")
			}
		case "HGET":
			if len(parts) >= 3 {
				if val, ok := cache.HGet(parts[1], parts[2]); ok {
					fmt.Fprintf(conn, "VALUE %s\n", val)
				} else {
					fmt.Fprintln(conn, "NIL")
				}
			}
		case "SUBSCRIBE":
			if len(parts) >= 2 {
				cache.Subscribe(parts[1], conn)
				fmt.Fprintf(conn, "SUBSCRIBED %s\n", parts[1])
			}
		case "PUBLISH":
			if len(parts) >= 3 {
				count := cache.Publish(parts[1], strings.Join(parts[2:], " "))
				fmt.Fprintf(conn, "PUBLISHED %d\n", count)
			}
		case "EVAL":

			if cache.LuaEngine == nil {
				fmt.Fprintln(conn, "ERROR: Lua scripting disabled")
				continue
			}

			if len(parts) >= 4 && cache.LuaEngine != nil {
				script := parts[1]
				numKeys, _ := strconv.Atoi(parts[2])
				keys := parts[3 : 3+numKeys]
				args := parts[3+numKeys:]
				result := cache.LuaEngine.Execute(script, keys, args)
				fmt.Fprintf(conn, "RESULT %v\n", result)
			}
		case "INFO":
			info := fmt.Sprintf(
				"BoltCache keys=%d lua=%v pubsub=%v mode=tcp",
				cache.Data.Len(),
				cache.LuaEngine != nil,
				cache.Config.Features.PubSub,
			)

			fmt.Fprintln(conn, info)

		default:
			fmt.Fprintln(conn, "ERROR: Unknown command")
		}
	}
}
