package main

import (
	"fmt"
	"strconv"
	"strings"
)

// Simple Lua-like scripting engine
type LuaEngine struct {
	cache *BoltCache
}

func NewLuaEngine(cache *BoltCache) *LuaEngine {
	return &LuaEngine{cache: cache}
}

func (l *LuaEngine) Execute(script string, keys []string, args []string) string {
	// Simple script interpreter
	lines := strings.Split(script, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "--") {
			continue
		}

		// Replace KEYS[1], KEYS[2] etc with actual keys
		for i, key := range keys {
			placeholder := fmt.Sprintf("KEYS[%d]", i+1)
			line = strings.ReplaceAll(line, placeholder, key)
		}

		// Replace ARGV[1], ARGV[2] etc with actual args
		for i, arg := range args {
			placeholder := fmt.Sprintf("ARGV[%d]", i+1)
			line = strings.ReplaceAll(line, placeholder, arg)
		}

		// Execute Redis-like commands
		if strings.HasPrefix(line, "redis.call") {
			cmd := extractCommand(line)
			result := l.executeCommand(cmd)
			if result != "" {
				return result
			}
		}
	}

	return "OK"
}

func extractCommand(line string) []string {
	// Extract command from redis.call('GET', 'key')
	start := strings.Index(line, "(")
	end := strings.LastIndex(line, ")")
	if start == -1 || end == -1 {
		return []string{}
	}

	content := line[start+1 : end]
	parts := strings.Split(content, ",")

	var cmd []string
	for _, part := range parts {
		part = strings.TrimSpace(part)
		part = strings.Trim(part, "'\"")
		cmd = append(cmd, part)
	}

	return cmd
}

func (l *LuaEngine) executeCommand(cmd []string) string {
	if len(cmd) == 0 {
		return ""
	}

	switch strings.ToUpper(cmd[0]) {
	case "GET":
		if len(cmd) >= 2 {
			if value, ok := l.cache.Get(cmd[1]); ok {
				if s, ok := value.(string); ok {
					return s
				}
				if b, ok := value.([]byte); ok {
					return string(b)
				}
				return fmt.Sprintf("%v", value)
			}
		}
		return "nil"

	case "SET":
		if len(cmd) >= 3 {
			l.cache.Set(cmd[1], cmd[2], 0)
			return "OK"
		}

	case "INCR":
		if len(cmd) >= 2 {
			key := cmd[1]
			if value, ok := l.cache.Get(key); ok {
				var valStr string
				if s, ok := value.(string); ok {
					valStr = s
				} else if b, ok := value.([]byte); ok {
					valStr = string(b)
				} else {
					valStr = fmt.Sprintf("%v", value)
				}

				if num, err := strconv.Atoi(valStr); err == nil {
					newVal := strconv.Itoa(num + 1)
					l.cache.Set(key, newVal, 0)
					return newVal
				}
			} else {
				l.cache.Set(key, "1", 0)
				return "1"
			}
		}
	}

	return ""
}
