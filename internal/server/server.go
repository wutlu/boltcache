// Package server wires the cache, command engine and network listeners
// (TCP text/RESP, RESP and HTTP) together and manages their lifecycle.
package server

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/wutlu/boltcache/appinfo"
	"github.com/wutlu/boltcache/config"
	"github.com/wutlu/boltcache/internal/auth"
	"github.com/wutlu/boltcache/internal/cache"
	"github.com/wutlu/boltcache/internal/command"
	"github.com/wutlu/boltcache/internal/pubsub"
)

// Server is a running BoltCache instance.
type Server struct {
	cfg       *config.Config
	log       *slog.Logger
	cache     *cache.BoltCache
	engine    *command.Engine
	persister *cache.Persister
	auth      *auth.Manager
	metrics   *metrics
	clients   atomic.Int64

	tcp  *protoServer
	resp *protoServer
	rest *restServer
}

// New validates cfg, builds the cache and restores persisted data.
func New(cfg *config.Config, log *slog.Logger) (*Server, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config:\n%w", err)
	}
	if log == nil {
		log = slog.Default()
	}
	if cfg.Performance.GCPercent > 0 {
		debug.SetGCPercent(cfg.Performance.GCPercent)
	}

	maxMem, _ := config.ParseMemorySize(cfg.Cache.MaxMemory)
	policy, err := cache.ParsePolicy(cfg.Cache.EvictionPolicy)
	if err != nil {
		return nil, err
	}
	c := cache.New(cache.Options{
		MaxKeys:           int64(cfg.Cache.MaxKeys),
		MaxMemory:         maxMem,
		EvictionThreshold: cfg.Cache.EvictionThreshold,
		EvictionPolicy:    policy,
		CleanupInterval:   cfg.Cache.CleanupInterval,
	})

	s := &Server{cfg: cfg, log: log, cache: c}
	s.auth = auth.NewManager(cfg.Security.Auth.Enabled, cfg.AuthTokens())
	s.engine = command.New(c, pubsub.NewHub(), command.Config{
		Auth:          s.auth,
		LuaEnabled:    cfg.Features.LuaScripting,
		PubSubEnabled: cfg.Features.PubSub,
		ScriptTimeout: cfg.Features.LuaTimeout,
		Version:       appinfo.Version,
	})
	s.engine.ClientCount = s.clients.Load

	if cfg.Persistence.Enabled {
		s.persister = cache.NewPersister(c, cache.PersistOptions{
			File:               cfg.Persistence.File,
			Interval:           cfg.Persistence.Interval,
			Compress:           cfg.Persistence.Compression,
			BackupCount:        cfg.Persistence.BackupCount,
			CleanupWhenExceeds: cfg.Persistence.CleanupWhenExceeds,
			Logf: func(format string, args ...any) {
				log.Warn(fmt.Sprintf(format, args...))
			},
		})
		start := time.Now()
		n, err := s.persister.Load()
		if err != nil {
			c.Close()
			return nil, fmt.Errorf("refusing to start with an unreadable snapshot (move it away to start empty): %w", err)
		}
		if n > 0 {
			log.Info("restored snapshot", "file", cfg.Persistence.File, "keys", n, "took", time.Since(start).Round(time.Millisecond))
		}
		s.engine.Persister = s.persister
	}

	s.metrics = &metrics{
		started:   time.Now(),
		version:   appinfo.Version,
		engine:    s.engine,
		persister: func() *cache.Persister { return s.persister },
		clients:   &s.clients,
	}
	return s, nil
}

// Engine returns the command engine (useful for embedding and tests).
func (s *Server) Engine() *command.Engine { return s.engine }

// Start binds all configured listeners. On error, already started
// listeners are stopped.
func (s *Server) Start() error {
	cfg := s.cfg
	s.log.Info("starting BoltCache", "version", appinfo.Version, "mode", cfg.Server.Mode,
		"auth", s.auth.Enabled(), "lua", cfg.Features.LuaScripting, "pubsub", cfg.Features.PubSub,
		"persistence", cfg.Persistence.Enabled)
	if !s.auth.Enabled() && !isLoopback(cfg.Server.TCP.Host) {
		s.log.Warn("authentication is disabled; anyone who can reach this server can read and modify data (security.auth)")
	}

	fail := func(err error) error {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.stopListeners(ctx)
		return err
	}
	if cfg.TCPEnabled() {
		s.tcp = newProtoServer("tcp", cfg.GetTCPAddress(), true, cfg.Server.TCP.MaxConnections, s.engine, s.log, &s.clients)
		if err := s.tcp.start(); err != nil {
			s.tcp = nil
			return fail(err)
		}
	}
	if cfg.Server.RESP.IsEnabled() {
		s.resp = newProtoServer("resp", cfg.GetRESPAddress(), false, cfg.Server.TCP.MaxConnections, s.engine, s.log, &s.clients)
		if err := s.resp.start(); err != nil {
			s.resp = nil
			return fail(err)
		}
	}
	if cfg.RESTEnabled() {
		s.rest = newRESTServer(cfg, s.engine, s.auth, s.metrics, s.log, appinfo.Version, &s.clients)
		if err := s.rest.start(cfg.GetRESTAddress()); err != nil {
			s.rest = nil
			return fail(err)
		}
	}
	if s.persister != nil {
		s.persister.Start()
	}
	return nil
}

func isLoopback(host string) bool {
	return host == "localhost" || strings.HasPrefix(host, "127.") || host == "::1"
}

func (s *Server) stopListeners(ctx context.Context) {
	if s.rest != nil {
		if err := s.rest.stop(ctx); err != nil {
			s.log.Warn("stopping rest listener", "err", err)
		}
	}
	for _, ps := range []*protoServer{s.tcp, s.resp} {
		if ps != nil {
			if err := ps.stop(ctx); err != nil {
				s.log.Warn("stopping listener", "listener", ps.name, "err", err)
			}
		}
	}
}

// Shutdown stops accepting connections, writes a final snapshot and stops
// background work.
func (s *Server) Shutdown(ctx context.Context) error {
	s.log.Info("shutting down")
	s.stopListeners(ctx)
	var err error
	if s.persister != nil {
		if err = s.persister.Stop(); err != nil {
			s.log.Error("final snapshot failed", "err", err)
		} else {
			s.log.Info("snapshot saved", "file", s.cfg.Persistence.File, "keys", s.cache.Len())
		}
	}
	s.cache.Close()
	s.log.Info("shutdown complete")
	return err
}

// Run starts the server and blocks until SIGINT/SIGTERM, then shuts down
// gracefully.
func (s *Server) Run() error {
	if err := s.Start(); err != nil {
		return err
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	signal.Stop(sig)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return s.Shutdown(ctx)
}
