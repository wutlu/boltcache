package server

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
	"syscall"

	"net"

	"github.com/gorilla/websocket"
)

import (
	config "boltcache/config"
	cache "boltcache/internal/cache"
	logger "boltcache/logger"
)

type Server struct {
	config *config.Config
	cache  *cache.BoltCache
}

func NewServer(configFile string) (*Server, error) {
	// Load configuration
	config, err := config.LoadConfig(configFile)
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
	logger.Log("Starting BoltCache server...")
	logger.Log("Mode: %s", s.config.Server.Mode)
	logger.Log("Features: Lua=%v, PubSub=%v, ComplexTypes=%v",
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
		StartGnetServer(s.cache, s.config)     // High-performance gnet on port 6381
		StartRESPGnetServer(s.cache, s.config) // RESP gnet (multicore) on port 6382
	case "rest":
		go s.startRESTServer()
		StartRESPGnetServer(s.cache, s.config) // RESP gnet (multicore) on port 6382
	case "both":
		go s.startTCPServer()
		go s.startRESTServer()
		StartGnetServer(s.cache, s.config)     // High-performance gnet on port 6381
		StartRESPGnetServer(s.cache, s.config) // RESP gnet (multicore) on port 6382
	default:
		return fmt.Errorf("invalid server mode: %s", s.config.Server.Mode)
	}

	// Start monitoring if enabled
	if s.config.Monitoring.Metrics.Enabled {
		go s.startMetricsServer()
	}

	// Wait for shutdown signal
	<-sigChan
	logger.Log("Shutting down server...")

	// Graceful shutdown
	s.shutdown()
	return nil
}

func (s *Server) startTCPServer() {
	addr := s.config.GetTCPAddress()
	logger.LogServerStartWithMsg("TCP server would start on %s", addr)

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
		go handleConnection(conn, s.cache)
	}
}

func (s *Server) startRESTServer() {
	restServer := NewRestServerWithConfig(s.cache, s.config)
	addr := s.config.GetRESTAddress()
	logger.LogServerStartWithMsg("REST server starting on %s", addr)

	if err := restServer.Start(); err != nil {
		log.Fatalf("Failed to start REST server: %v", err)
	}
}

func (s *Server) startMetricsServer() {
	// Metrics server implementation
	logger.LogServerStartWithMsg("Metrics server starting on %s", s.config.Monitoring.Metrics.Endpoint)
}

func (s *Server) shutdown() {
	logger.Log("Performing graceful shutdown...")

	// Save data if persistence is enabled
	if s.config.Persistence.Enabled {
		logger.Log("Saving data to disk...")
		s.cache.ForcePersist()
	}

	logger.Log("Shutdown complete")
}

func applyPerformanceSettings(config *config.Config) {
	// Set GC percentage
	if config.Performance.GCPercent > 0 {
		debug.SetGCPercent(config.Performance.GCPercent)
		logger.Log("Set GC percent to %d", config.Performance.GCPercent)
	}

	// Set max goroutines (via GOMAXPROCS)
	if config.Performance.MaxGoroutines > 0 {
		// if GOMAXPROCS load in config use this
		// 		runtime.GOMAXPROCS(config.Performance.MaxGoroutines)
		// 		logger.Log("Set GOMAXPROCS to %d", config.Performance.MaxGoroutines)

		runtime.GOMAXPROCS(runtime.NumCPU())
		logger.Log("Set GOMAXPROCS to %d", runtime.NumCPU())
	}

	logger.Log("Applied performance settings")
}

// Enhanced BoltCache constructor with config
func NewBoltCacheWithConfig(config *config.Config) *cache.BoltCache {
	_cache := &cache.BoltCache{
		Data:        cache.NewShardedMap(),
		PersistFile: config.Persistence.File,
		Config:      config,
	}

	// Initialize Lua engine if enabled
	if config.Features.LuaScripting {
		_cache.LuaEngine = cache.NewLuaEngine(_cache)
	}

	// Load from disk if persistence enabled
	if config.Persistence.Enabled {
		_cache.LoadFromDisk()
	}

	// Start background tasks
	go _cache.CleanupExpiredWithConfig()

	if config.Persistence.Enabled {
		go _cache.PersistToDiskWithConfig()
	}

	return _cache
}

// Enhanced REST server with config
func NewRestServerWithConfig(cache *cache.BoltCache, config *config.Config) *RestServer {
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