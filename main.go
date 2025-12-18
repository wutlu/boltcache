package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type DataType int

const (
	String DataType = iota
	List
	Set
	Hash
)

type CacheItem struct {
	Value     interface{}
	Type      DataType
	ExpiresAt time.Time
}

type Subscriber struct {
	Channel chan string
	Conn    net.Conn
}

type BoltCache struct {
	data        sync.Map
	subscribers sync.Map
	mu          sync.RWMutex
	persistFile string
	replicas    []string
	luaEngine   *LuaEngine
	config      *Config
}

func NewBoltCache(persistFile string) *BoltCache {
	cache := &BoltCache{
		persistFile: persistFile,
	}
	cache.luaEngine = NewLuaEngine(cache)
	cache.loadFromDisk()
	go cache.cleanupExpired()
	go cache.persistToDisk()
	return cache
}

func (c *BoltCache) loadFromDisk() {
	if c.persistFile == "" {
		return
	}
	data, err := os.ReadFile(c.persistFile)
	if err != nil {
		return
	}
	var items map[string]*CacheItem
	if err := json.Unmarshal(data, &items); err != nil {
		return
	}
	for k, v := range items {
		c.data.Store(k, v)
	}
}

func (c *BoltCache) persistToDisk() {
	if c.persistFile == "" {
		return
	}
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	
	for range ticker.C {
		items := make(map[string]*CacheItem)
		c.data.Range(func(key, value interface{}) bool {
			items[key.(string)] = value.(*CacheItem)
			return true
		})
		
		data, err := json.Marshal(items)
		if err != nil {
			continue
		}
		
		os.MkdirAll(filepath.Dir(c.persistFile), 0755)
		os.WriteFile(c.persistFile, data, 0644)
	}
}

func (c *BoltCache) Set(key, value string, ttl time.Duration) {
	expiresAt := time.Time{}
	if ttl > 0 {
		expiresAt = time.Now().Add(ttl)
	}
	c.data.Store(key, &CacheItem{Value: value, Type: String, ExpiresAt: expiresAt})
	c.replicateCommand(fmt.Sprintf("SET %s %s", key, value))
}

func (c *BoltCache) Get(key string) (string, bool) {
	val, ok := c.data.Load(key)
	if !ok {
		return "", false
	}
	
	item := val.(*CacheItem)
	if !item.ExpiresAt.IsZero() && time.Now().After(item.ExpiresAt) {
		c.data.Delete(key)
		return "", false
	}
	
	if item.Type == String {
		return item.Value.(string), true
	}
	return "", false
}

// List operations
func (c *BoltCache) LPush(key string, values ...string) int {
	val, _ := c.data.LoadOrStore(key, &CacheItem{Value: []string{}, Type: List})
	item := val.(*CacheItem)
	
	// Handle type conversion for data loaded from JSON
	var list []string
	switch v := item.Value.(type) {
	case []string:
		list = v
	case []interface{}:
		// Convert from JSON unmarshaling
		list = make([]string, len(v))
		for i, val := range v {
			list[i] = val.(string)
		}
	case string:
		// Single string value, convert to slice
		list = []string{v}
	default:
		// Initialize empty list
		list = []string{}
	}
	
	list = append(values, list...)
	item.Value = list
	item.Type = List
	c.data.Store(key, item)
	return len(list)
}

func (c *BoltCache) LPop(key string) (string, bool) {
	val, ok := c.data.Load(key)
	if !ok {
		return "", false
	}
	item := val.(*CacheItem)
	if item.Type != List {
		return "", false
	}
	
	// Handle type conversion for data loaded from JSON
	var list []string
	switch v := item.Value.(type) {
	case []string:
		list = v
	case []interface{}:
		// Convert from JSON unmarshaling
		list = make([]string, len(v))
		for i, val := range v {
			list[i] = val.(string)
		}
	default:
		return "", false
	}
	
	if len(list) == 0 {
		return "", false
	}
	result := list[0]
	item.Value = list[1:]
	c.data.Store(key, item)
	return result, true
}

// Set operations
func (c *BoltCache) SAdd(key string, members ...string) int {
	val, _ := c.data.LoadOrStore(key, &CacheItem{Value: make(map[string]bool), Type: Set})
	item := val.(*CacheItem)
	
	// Handle type conversion for data loaded from JSON
	var set map[string]bool
	switch v := item.Value.(type) {
	case map[string]bool:
		set = v
	case map[string]interface{}:
		// Convert from JSON unmarshaling
		set = make(map[string]bool)
		for k, val := range v {
			set[k] = val.(bool)
		}
	default:
		// Initialize empty set
		set = make(map[string]bool)
	}
	
	count := 0
	for _, member := range members {
		if !set[member] {
			set[member] = true
			count++
		}
	}
	item.Value = set
	item.Type = Set
	c.data.Store(key, item)
	return count
}

func (c *BoltCache) SMembers(key string) []string {
	val, ok := c.data.Load(key)
	if !ok {
		return []string{}
	}
	item := val.(*CacheItem)
	if item.Type != Set {
		return []string{}
	}
	
	// Handle type conversion for data loaded from JSON
	var set map[string]bool
	switch v := item.Value.(type) {
	case map[string]bool:
		set = v
	case map[string]interface{}:
		// Convert from JSON unmarshaling
		set = make(map[string]bool)
		for k, val := range v {
			set[k] = val.(bool)
		}
	default:
		return []string{}
	}
	
	members := make([]string, 0, len(set))
	for member := range set {
		members = append(members, member)
	}
	return members
}

// Hash operations
func (c *BoltCache) HSet(key, field, value string) {
	val, _ := c.data.LoadOrStore(key, &CacheItem{Value: make(map[string]string), Type: Hash})
	item := val.(*CacheItem)
	
	// Handle type conversion for data loaded from JSON
	var hash map[string]string
	switch v := item.Value.(type) {
	case map[string]string:
		hash = v
	case map[string]interface{}:
		// Convert from JSON unmarshaling
		hash = make(map[string]string)
		for k, val := range v {
			hash[k] = val.(string)
		}
	default:
		// Initialize empty hash
		hash = make(map[string]string)
	}
	
	hash[field] = value
	item.Value = hash
	item.Type = Hash
	c.data.Store(key, item)
}

func (c *BoltCache) HGet(key, field string) (string, bool) {
	val, ok := c.data.Load(key)
	if !ok {
		return "", false
	}
	item := val.(*CacheItem)
	if item.Type != Hash {
		return "", false
	}
	
	// Handle type conversion for data loaded from JSON
	var hash map[string]string
	switch v := item.Value.(type) {
	case map[string]string:
		hash = v
	case map[string]interface{}:
		// Convert from JSON unmarshaling
		hash = make(map[string]string)
		for k, val := range v {
			hash[k] = val.(string)
		}
	default:
		return "", false
	}
	
	value, exists := hash[field]
	return value, exists
}

func (c *BoltCache) Delete(key string) {
	c.data.Delete(key)
	c.replicateCommand(fmt.Sprintf("DEL %s", key))
}

func (c *BoltCache) Subscribe(channel string, conn net.Conn) {
	sub := &Subscriber{
		Channel: make(chan string, 100),
		Conn:    conn,
	}
	
	val, _ := c.subscribers.LoadOrStore(channel, make([]*Subscriber, 0))
	subs := val.([]*Subscriber)
	subs = append(subs, sub)
	c.subscribers.Store(channel, subs)
	
	go func() {
		for msg := range sub.Channel {
			fmt.Fprintf(conn, "MESSAGE %s %s\n", channel, msg)
		}
	}()
}

func (c *BoltCache) Publish(channel, message string) int {
	val, ok := c.subscribers.Load(channel)
	if !ok {
		return 0
	}
	
	subs := val.([]*Subscriber)
	count := 0
	for _, sub := range subs {
		select {
		case sub.Channel <- message:
			count++
		default:
			// Skip if channel is full
		}
	}
	c.replicateCommand(fmt.Sprintf("PUBLISH %s %s", channel, message))
	return count
}

func (c *BoltCache) AddReplica(address string) {
	c.replicas = append(c.replicas, address)
}

func (c *BoltCache) replicateCommand(command string) {
	for _, replica := range c.replicas {
		go func(addr string) {
			conn, err := net.Dial("tcp", addr)
			if err != nil {
				return
			}
			defer conn.Close()
			fmt.Fprintln(conn, command)
		}(replica)
	}
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
			log.Printf("Cleaned up %d expired keys", len(expiredKeys))
		}
	}
}

func (c *BoltCache) handleConnection(conn net.Conn) {
	defer conn.Close()
	scanner := bufio.NewScanner(conn)
	
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		parts := strings.Fields(line)
		
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
					fmt.Fprintf(conn, "VALUE %s\n", value)
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
			} else {
				fmt.Fprintln(conn, "ERROR: DEL key")
			}
			
		// List operations
		case "LPUSH":
			if len(parts) >= 3 {
				key := parts[1]
				values := parts[2:]
				count := c.LPush(key, values...)
				fmt.Fprintf(conn, "INTEGER %d\n", count)
			} else {
				fmt.Fprintln(conn, "ERROR: LPUSH key value [value ...]")
			}
			
		case "LPOP":
			if len(parts) >= 2 {
				key := parts[1]
				if value, ok := c.LPop(key); ok {
					fmt.Fprintf(conn, "VALUE %s\n", value)
				} else {
					fmt.Fprintln(conn, "NIL")
				}
			} else {
				fmt.Fprintln(conn, "ERROR: LPOP key")
			}
			
		// Set operations
		case "SADD":
			if len(parts) >= 3 {
				key := parts[1]
				members := parts[2:]
				count := c.SAdd(key, members...)
				fmt.Fprintf(conn, "INTEGER %d\n", count)
			} else {
				fmt.Fprintln(conn, "ERROR: SADD key member [member ...]")
			}
			
		case "SMEMBERS":
			if len(parts) >= 2 {
				key := parts[1]
				members := c.SMembers(key)
				fmt.Fprintf(conn, "ARRAY %s\n", strings.Join(members, " "))
			} else {
				fmt.Fprintln(conn, "ERROR: SMEMBERS key")
			}
			
		// Hash operations
		case "HSET":
			if len(parts) >= 4 {
				key, field, value := parts[1], parts[2], parts[3]
				c.HSet(key, field, value)
				fmt.Fprintln(conn, "OK")
			} else {
				fmt.Fprintln(conn, "ERROR: HSET key field value")
			}
			
		case "HGET":
			if len(parts) >= 3 {
				key, field := parts[1], parts[2]
				if value, ok := c.HGet(key, field); ok {
					fmt.Fprintf(conn, "VALUE %s\n", value)
				} else {
					fmt.Fprintln(conn, "NIL")
				}
			} else {
				fmt.Fprintln(conn, "ERROR: HGET key field")
			}
			
		case "SUBSCRIBE":
			if len(parts) >= 2 {
				channel := parts[1]
				c.Subscribe(channel, conn)
				fmt.Fprintf(conn, "SUBSCRIBED %s\n", channel)
			} else {
				fmt.Fprintln(conn, "ERROR: SUBSCRIBE channel")
			}
			
		case "PUBLISH":
			if len(parts) >= 3 {
				channel, message := parts[1], strings.Join(parts[2:], " ")
				count := c.Publish(channel, message)
				fmt.Fprintf(conn, "PUBLISHED %d\n", count)
			} else {
				fmt.Fprintln(conn, "ERROR: PUBLISH channel message")
			}
			
		case "EVAL":
			if len(parts) >= 4 {
				script := parts[1]
				numKeys, _ := strconv.Atoi(parts[2])
				keys := parts[3:3+numKeys]
				args := parts[3+numKeys:]
				result := c.luaEngine.Execute(script, keys, args)
				fmt.Fprintf(conn, "RESULT %s\n", result)
			} else {
				fmt.Fprintln(conn, "ERROR: EVAL script numkeys key [key ...] arg [arg ...]")
			}
			
		case "INFO":
			var count int
			c.data.Range(func(k, v interface{}) bool {
				count++
				return true
			})
			fmt.Fprintf(conn, "KEYS %d\nREPLICAS %d\n", count, len(c.replicas))
			
		case "PING":
			fmt.Fprintln(conn, "PONG")
			
		default:
			fmt.Fprintln(conn, "ERROR: Unknown command")
		}
	}
}

