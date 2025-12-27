package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/gorilla/websocket"

	bcLogger "boltcache/logger"
)

type Server struct {
	config *Config
	cache  *BoltCache
}

func NewServer(configFile string) (*Server, error) {
	// Load configuration
	config, err := LoadConfig(configFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %v", err)
	}

	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %v", err)
	}

	// Apply performance settings
	applyPerformanceSettings(config)

	// Create cache with config
	cache := NewBoltCacheWithConfig(config)

	return &Server{
		config: config,
		cache:  cache,
	}, nil
}

func (s *Server) Start() error {
	bcLogger.Log("Starting BoltCache server...")
	bcLogger.Log("Mode: %s", s.config.Server.Mode)
	bcLogger.Log("Features: Lua=%v, PubSub=%v, ComplexTypes=%v",
		s.config.Features.LuaScripting,
		s.config.Features.PubSub,
		s.config.Features.ComplexTypes)

	// Setup graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start servers based on mode
	switch s.config.Server.Mode {
	case "tcp":
		go s.startTCPServer()
		StartGnetServer(s.cache)     // High-performance gnet on port 6381
		StartRESPGnetServer(s.cache) // RESP gnet (multicore) on port 6382
	case "rest":
		go s.startRESTServer()
		StartRESPGnetServer(s.cache) // RESP gnet (multicore) on port 6382
	case "both":
		go s.startTCPServer()
		go s.startRESTServer()
		StartGnetServer(s.cache)     // High-performance gnet on port 6381
		StartRESPGnetServer(s.cache) // RESP gnet (multicore) on port 6382
	default:
		return fmt.Errorf("invalid server mode: %s", s.config.Server.Mode)
	}

	// Start monitoring if enabled
	if s.config.Monitoring.Metrics.Enabled {
		go s.startMetricsServer()
	}

	// Wait for shutdown signal
	<-sigChan
	bcLogger.Log("Shutting down server...")

	// Graceful shutdown
	s.shutdown()
	return nil
}

func (s *Server) startTCPServer() {
	addr := s.config.GetTCPAddress()
	bcLogger.LogServerStartWithMsg("TCP server would start on %s", addr)

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("Failed to start TCP server: %v", err)
	}
	defer listener.Close()

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("Failed to accept connection: %v", err)
			continue
		}
		go s.cache.handleConnection(conn)
	}
}

func (s *Server) startRESTServer() {
	restServer := NewRestServerWithConfig(s.cache, s.config)
	addr := s.config.GetRESTAddress()
	bcLogger.LogServerStartWithMsg("REST server starting on %s", addr)

	if err := restServer.Start(); err != nil {
		log.Fatalf("Failed to start REST server: %v", err)
	}
}

func (s *Server) startMetricsServer() {
	// Metrics server implementation
	bcLogger.LogServerStartWithMsg("Metrics server starting on %s", s.config.Monitoring.Metrics.Endpoint)
}

func (s *Server) shutdown() {
	bcLogger.Log("Performing graceful shutdown...")

	// Save data if persistence is enabled
	if s.config.Persistence.Enabled {
		bcLogger.Log("Saving data to disk...")
		s.cache.forcePersist()
	}

	bcLogger.Log("Shutdown complete")
}

func applyPerformanceSettings(config *Config) {
	// Set GC percentage
	if config.Performance.GCPercent > 0 {
		debug.SetGCPercent(config.Performance.GCPercent)
		bcLogger.Log("Set GC percent to %d", config.Performance.GCPercent)
	}

	// Set max goroutines (via GOMAXPROCS)
	if config.Performance.MaxGoroutines > 0 {
		// if GOMAXPROCS load in config use this
		// 		runtime.GOMAXPROCS(config.Performance.MaxGoroutines)
		// 		bcLogger.Log("Set GOMAXPROCS to %d", config.Performance.MaxGoroutines)

		runtime.GOMAXPROCS(runtime.NumCPU())
		bcLogger.Log("Set GOMAXPROCS to %d", runtime.NumCPU())
	}

	bcLogger.Log("Applied performance settings")
}

// Enhanced BoltCache constructor with config
func NewBoltCacheWithConfig(config *Config) *BoltCache {
	cache := &BoltCache{
		data:        NewShardedMap(),
		persistFile: config.Persistence.File,
		config:      config,
	}

	// Initialize Lua engine if enabled
	if config.Features.LuaScripting {
		cache.luaEngine = NewLuaEngine(cache)
	}

	// Load from disk if persistence enabled
	if config.Persistence.Enabled {
		cache.loadFromDisk()
	}

	// Start background tasks
	go cache.cleanupExpiredWithConfig()

	if config.Persistence.Enabled {
		go cache.persistToDiskWithConfig()
	}

	return cache
}

// Enhanced REST server with config
func NewRestServerWithConfig(cache *BoltCache, config *Config) *RestServer {
	return &RestServer{
		cache:  cache,
		config: config,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return config.Server.REST.CORSEnabled
			},
		},
	}
}

// Enhanced cache methods with config
func (c *BoltCache) cleanupExpiredWithConfig() {
	interval := c.config.Cache.CleanupInterval
	if interval == 0 {
		interval = time.Minute
	}

	ticker := time.NewTicker(interval)
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
			c.data.Delete(key.(string))
		}

		if len(expiredKeys) > 0 {
			bcLogger.Log("Cleaned up %d expired keys", len(expiredKeys))
		}
	}
}

func (c *BoltCache) persistToDiskWithConfig() {
	if !c.config.Persistence.Enabled {
		return
	}

	interval := c.config.Persistence.Interval
	if interval == 0 {
		interval = 30 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		c.forcePersist()
	}
}

func (c *BoltCache) forcePersist() {
	if !c.config.Persistence.Enabled {
		return
	}

	items := make(map[string]*CacheItem)
	c.data.Range(func(key, value interface{}) bool {
		items[key.(string)] = value.(*CacheItem)
		return true
	})

	data, err := json.Marshal(items)
	if err != nil {
		bcLogger.Log("Failed to marshal data: %v", err)
		return
	}

	// Create backup if enabled
	if c.config.Persistence.BackupCount > 0 {
		c.createBackup()
	}

	// Write to file
	os.MkdirAll(filepath.Dir(c.config.Persistence.File), 0755)
	if err := os.WriteFile(c.config.Persistence.File, data, 0644); err != nil {
		bcLogger.Log("Failed to write persistence file: %v", err)
	}
}

func (c *BoltCache) createBackup() {
	// Backup implementation
	backupFile := c.config.Persistence.File + ".backup." + time.Now().Format("20060102-150405")

	if data, err := os.ReadFile(c.config.Persistence.File); err == nil {
		os.WriteFile(backupFile, data, 0644)
	}
}
