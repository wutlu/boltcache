package config

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

var GlobalConfig *Config

// Default returns the built-in configuration used for any setting missing
// from the config file.
func Default() *Config {
	return &Config{
		Server: ServerConfig{
			Mode: "both",
			TCP: TCPConfig{
				Host:           "0.0.0.0",
				Port:           6380,
				MaxConnections: 10000,
				ReadTimeout:    30 * time.Second,
				WriteTimeout:   30 * time.Second,
			},
			RESP: RESPConfig{
				Host: "0.0.0.0",
				Port: 6382,
			},
			REST: RESTConfig{
				Host:           "0.0.0.0",
				Port:           8090,
				CORSEnabled:    true,
				CORSOrigins:    []string{"*"},
				RequestTimeout: 30 * time.Second,
				MaxRequestSize: "10MB",
			},
		},
		Cache: CacheConfig{
			MaxMemory:         "0",
			MaxKeys:           0,
			DefaultTTL:        0,
			MaxTTL:            0,
			CleanupInterval:   time.Minute,
			EvictionPolicy:    "lru",
			EvictionThreshold: 1.0,
		},
		Persistence: PersistenceConfig{
			Enabled:            true,
			File:               "./data/boltcache.json",
			Interval:           30 * time.Second,
			Compression:        false,
			BackupCount:        3,
			CleanupWhenExceeds: 0,
		},
		Security: SecurityConfig{
			Auth:      AuthConfig{Method: "token"},
			RateLimit: RateLimitConfig{RequestsPerSecond: 1000, Burst: 100},
		},
		Logging: LoggingConfig{Level: "info", Format: "text"},
		Monitoring: MonitoringConfig{
			Metrics:   MetricsConfig{Enabled: true, Endpoint: "/metrics"},
			Health:    HealthConfig{Enabled: true, Endpoint: "/health"},
			Profiling: ProfilingConfig{Endpoint: "/debug/pprof"},
		},
		Features: FeaturesConfig{
			LuaScripting: true,
			LuaTimeout:   5 * time.Second,
			PubSub:       true,
			ComplexTypes: true,
		},
	}
}

// LoadConfig reads filename over the defaults. A missing file yields the
// defaults. ${VAR} and ${VAR:-default} references are expanded from the
// environment before parsing, so secrets can be injected without editing
// the file.
func LoadConfig(filename string) (*Config, error) {
	cfg := Default()
	if filename != "" {
		data, err := os.ReadFile(filename)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return nil, fmt.Errorf("failed to read config file: %w", err)
			}
		} else {
			if err := yaml.Unmarshal([]byte(ExpandEnv(string(data))), cfg); err != nil {
				return nil, fmt.Errorf("failed to parse config file %s: %w", filename, err)
			}
		}
	}
	GlobalConfig = cfg
	return cfg, nil
}

var envRef = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(:-([^}]*))?\}`)

// ExpandEnv replaces ${VAR} and ${VAR:-default}. Unlike os.ExpandEnv it
// leaves bare $VAR alone, so values such as passwords containing '$' are
// not mangled.
func ExpandEnv(s string) string {
	return envRef.ReplaceAllStringFunc(s, func(m string) string {
		parts := envRef.FindStringSubmatch(m)
		if v, ok := os.LookupEnv(parts[1]); ok && (v != "" || parts[2] == "") {
			return v
		}
		return parts[3]
	})
}

// Validate checks the configuration for errors.
func (c *Config) Validate() error {
	var errs []error
	add := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }

	switch c.Server.Mode {
	case "tcp", "rest", "both":
	default:
		add("server.mode must be tcp, rest or both (got %q)", c.Server.Mode)
	}
	checkPort := func(name string, p int) {
		if p < 1 || p > 65535 {
			add("%s must be between 1 and 65535 (got %d)", name, p)
		}
	}
	checkPort("server.tcp.port", c.Server.TCP.Port)
	checkPort("server.rest.port", c.Server.REST.Port)
	if c.Server.RESP.IsEnabled() {
		checkPort("server.resp.port", c.Server.RESP.Port)
		if c.Server.RESP.Port == c.Server.TCP.Port && c.Server.Mode != "rest" {
			add("server.resp.port and server.tcp.port must differ (both %d)", c.Server.TCP.Port)
		}
	}
	if _, err := ParseMemorySize(c.Server.REST.MaxRequestSize); err != nil {
		add("server.rest.max_request_size: %v", err)
	}

	switch strings.ToLower(c.Cache.EvictionPolicy) {
	case "lru", "lfu", "random", "ttl", "noeviction", "":
	default:
		add("cache.eviction_policy must be lru, lfu, random, ttl or noeviction (got %q)", c.Cache.EvictionPolicy)
	}
	if c.Cache.EvictionThreshold < 0.1 || c.Cache.EvictionThreshold > 1.0 {
		add("cache.eviction_threshold must be between 0.1 and 1.0 (got %v)", c.Cache.EvictionThreshold)
	}
	if _, err := ParseMemorySize(c.Cache.MaxMemory); err != nil {
		add("cache.max_memory: %v", err)
	}
	if c.Cache.MaxKeys < 0 {
		add("cache.max_keys must be >= 0")
	}
	if c.Cache.DefaultTTL < 0 || c.Cache.MaxTTL < 0 {
		add("cache.default_ttl and cache.max_ttl must be >= 0")
	}
	if c.Persistence.Enabled && c.Persistence.File == "" {
		add("persistence.file is required when persistence is enabled")
	}
	if c.Security.Auth.Enabled && len(c.AuthTokens()) == 0 {
		add("security.auth.enabled requires at least one non-empty token in security.auth.tokens")
	}
	if c.Security.TLS.Enabled && (c.Security.TLS.CertFile == "" || c.Security.TLS.KeyFile == "") {
		add("security.tls requires cert_file and key_file")
	}
	if c.Security.RateLimit.Enabled && c.Security.RateLimit.RequestsPerSecond <= 0 {
		add("security.rate_limit.requests_per_second must be > 0")
	}
	switch strings.ToLower(c.Logging.Level) {
	case "debug", "info", "warn", "warning", "error", "":
	default:
		add("logging.level must be debug, info, warn or error (got %q)", c.Logging.Level)
	}
	switch strings.ToLower(c.Logging.Format) {
	case "text", "json", "":
	default:
		add("logging.format must be text or json (got %q)", c.Logging.Format)
	}
	if c.Features.LuaTimeout < 0 {
		add("features.lua_timeout must be >= 0")
	}
	return errors.Join(errs...)
}

// AuthTokens returns the configured tokens, ignoring empty entries (which
// appear when a ${VAR} reference is unset).
func (c *Config) AuthTokens() []string {
	var out []string
	for _, t := range c.Security.Auth.Tokens {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func (c *Config) GetTCPAddress() string {
	return fmt.Sprintf("%s:%d", c.Server.TCP.Host, c.Server.TCP.Port)
}

func (c *Config) GetRESPAddress() string {
	host := c.Server.RESP.Host
	if host == "" {
		host = c.Server.TCP.Host
	}
	return fmt.Sprintf("%s:%d", host, c.Server.RESP.Port)
}

func (c *Config) GetRESTAddress() string {
	return fmt.Sprintf("%s:%d", c.Server.REST.Host, c.Server.REST.Port)
}

// TCPEnabled reports whether the TCP (text + RESP) listener runs.
func (c *Config) TCPEnabled() bool { return c.Server.Mode == "tcp" || c.Server.Mode == "both" }

// RESTEnabled reports whether the HTTP API runs.
func (c *Config) RESTEnabled() bool { return c.Server.Mode == "rest" || c.Server.Mode == "both" }

func (c *Config) IsFeatureEnabled(feature string) bool {
	switch feature {
	case "lua_scripting":
		return c.Features.LuaScripting
	case "pub_sub":
		return c.Features.PubSub
	case "complex_types":
		return c.Features.ComplexTypes
	case "transactions":
		return c.Features.Transactions
	case "geo_commands":
		return c.Features.GeoCommands
	case "streams":
		return c.Features.Streams
	default:
		return false
	}
}

var sizeRe = regexp.MustCompile(`^\s*([0-9]+(?:\.[0-9]+)?)\s*([kKmMgGtT]?)(i?[bB])?\s*$`)

// ParseMemorySize parses sizes such as "512MB", "1.5GB", "64kb", "100" or
// "0". Units are binary (1KB = 1024 bytes). An empty string means 0.
func ParseMemorySize(size string) (int64, error) {
	if strings.TrimSpace(size) == "" {
		return 0, nil
	}
	m := sizeRe.FindStringSubmatch(size)
	if m == nil {
		return 0, fmt.Errorf("invalid size %q (examples: 512MB, 1GB, 1024)", size)
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size %q", size)
	}
	mult := map[string]float64{"": 1, "k": 1 << 10, "m": 1 << 20, "g": 1 << 30, "t": 1 << 40}[strings.ToLower(m[2])]
	return int64(v * mult), nil
}
