package config

import "os"

func GenerateDefaultConfig(filename string) error {
	defaultConfig := `# BoltCache Configuration File
# ============================

# Server Configuration
server:
  # Server mode: tcp, rest, both
  mode: "rest"
  swaggerui: true
  
  # TCP Server Settings
  tcp:
    host: "0.0.0.0"
    port: 6380
    max_connections: 1000
    read_timeout: "30s"
    write_timeout: "30s"
    
  # REST API Settings  
  rest:
    host: "0.0.0.0"
    port: 8080
    cors_enabled: true
    cors_origins: ["*"]
    request_timeout: "30s"
    max_request_size: "10MB"

# Cache Configuration
cache:
  # Memory limits
  max_memory: "1GB"
  max_keys: 1000000
  
  # TTL Settings
  default_ttl: "0s"  # 0 = no expiration
  max_ttl: "24h"
  cleanup_interval: "1m"
  
  # Eviction policy: lru, lfu, random, ttl
  eviction_policy: "lru"
  eviction_threshold: 0.8  # Start evicting at 80% memory

# Persistence Configuration
persistence:
  enabled: true
  file: "./data/boltcache.json"
  interval: "30s"
  compression: true
  backup_count: 3

# Security Configuration
security:
  # Rate limiting
  rate_limit:
    enabled: true
    requests_per_second: 1000
    burst: 100

# Logging Configuration
logging:
  level: "info"  # debug, info, warn, error
  format: "json"  # json, text
  file: "./logs/boltcache.log"

# Performance Tuning
performance:
  # Goroutine limits
  max_goroutines: 10000
  
  # GC tuning
  gc_percent: 100

# Feature Flags
features:
  lua_scripting: true
  pub_sub: true
  complex_types: true
`

	return os.WriteFile(filename, []byte(defaultConfig), 0644)
}
