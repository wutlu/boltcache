package main

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server      ServerConfig      `yaml:"server"`
	Cache       CacheConfig       `yaml:"cache"`
	Persistence PersistenceConfig `yaml:"persistence"`
	Cluster     ClusterConfig     `yaml:"cluster"`
	Security    SecurityConfig    `yaml:"security"`
	Logging     LoggingConfig     `yaml:"logging"`
	Monitoring  MonitoringConfig  `yaml:"monitoring"`
	Performance PerformanceConfig `yaml:"performance"`
	Features    FeaturesConfig    `yaml:"features"`
}

type ServerConfig struct {
	Mode string    `yaml:"mode"`
	TCP  TCPConfig `yaml:"tcp"`
	REST RESTConfig `yaml:"rest"`
}

type TCPConfig struct {
	Host           string        `yaml:"host"`
	Port           int           `yaml:"port"`
	MaxConnections int           `yaml:"max_connections"`
	ReadTimeout    time.Duration `yaml:"read_timeout"`
	WriteTimeout   time.Duration `yaml:"write_timeout"`
}

type RESTConfig struct {
	Host           string        `yaml:"host"`
	Port           int           `yaml:"port"`
	CORSEnabled    bool          `yaml:"cors_enabled"`
	CORSOrigins    []string      `yaml:"cors_origins"`
	RequestTimeout time.Duration `yaml:"request_timeout"`
	MaxRequestSize string        `yaml:"max_request_size"`
}

type CacheConfig struct {
	MaxMemory         string        `yaml:"max_memory"`
	MaxKeys           int           `yaml:"max_keys"`
	DefaultTTL        time.Duration `yaml:"default_ttl"`
	MaxTTL            time.Duration `yaml:"max_ttl"`
	CleanupInterval   time.Duration `yaml:"cleanup_interval"`
	EvictionPolicy    string        `yaml:"eviction_policy"`
	EvictionThreshold float64       `yaml:"eviction_threshold"`
}

type PersistenceConfig struct {
	Enabled     bool           `yaml:"enabled"`
	File        string         `yaml:"file"`
	Interval    time.Duration  `yaml:"interval"`
	Compression bool           `yaml:"compression"`
	BackupCount int            `yaml:"backup_count"`
	Snapshot    SnapshotConfig `yaml:"snapshot"`
}

type SnapshotConfig struct {
	Enabled   bool          `yaml:"enabled"`
	Interval  time.Duration `yaml:"interval"`
	Directory string        `yaml:"directory"`
}

type ClusterConfig struct {
	Enabled     bool              `yaml:"enabled"`
	NodeID      string            `yaml:"node_id"`
	Replication ReplicationConfig `yaml:"replication"`
	Discovery   DiscoveryConfig   `yaml:"discovery"`
}

type ReplicationConfig struct {
	Enabled  bool     `yaml:"enabled"`
	Mode     string   `yaml:"mode"`
	Replicas []string `yaml:"replicas"`
}

type DiscoveryConfig struct {
	Method    string   `yaml:"method"`
	Endpoints []string `yaml:"endpoints"`
}

type SecurityConfig struct {
	Auth      AuthConfig      `yaml:"auth"`
	TLS       TLSConfig       `yaml:"tls"`
	RateLimit RateLimitConfig `yaml:"rate_limit"`
}

type AuthConfig struct {
	Enabled bool     `yaml:"enabled"`
	Method  string   `yaml:"method"`
	Tokens  []string `yaml:"tokens"`
}

type TLSConfig struct {
	Enabled  bool   `yaml:"enabled"`
	CertFile string `yaml:"cert_file"`
	KeyFile  string `yaml:"key_file"`
}

type RateLimitConfig struct {
	Enabled           bool `yaml:"enabled"`
	RequestsPerSecond int  `yaml:"requests_per_second"`
	Burst             int  `yaml:"burst"`
}

type LoggingConfig struct {
	Level      string `yaml:"level"`
	Format     string `yaml:"format"`
	File       string `yaml:"file"`
	MaxSize    string `yaml:"max_size"`
	MaxBackups int    `yaml:"max_backups"`
	Compress   bool   `yaml:"compress"`
}

type MonitoringConfig struct {
	Metrics   MetricsConfig   `yaml:"metrics"`
	Health    HealthConfig    `yaml:"health"`
	Profiling ProfilingConfig `yaml:"profiling"`
}

type MetricsConfig struct {
	Enabled  bool          `yaml:"enabled"`
	Endpoint string        `yaml:"endpoint"`
	Interval time.Duration `yaml:"interval"`
}

type HealthConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Endpoint string `yaml:"endpoint"`
}

type ProfilingConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Endpoint string `yaml:"endpoint"`
}

type PerformanceConfig struct {
	MaxGoroutines    int                  `yaml:"max_goroutines"`
	ReadBufferSize   string               `yaml:"read_buffer_size"`
	WriteBufferSize  string               `yaml:"write_buffer_size"`
	GCPercent        int                  `yaml:"gc_percent"`
	ConnectionPool   ConnectionPoolConfig `yaml:"connection_pool"`
}

type ConnectionPoolConfig struct {
	MaxIdle     int           `yaml:"max_idle"`
	MaxActive   int           `yaml:"max_active"`
	IdleTimeout time.Duration `yaml:"idle_timeout"`
}

type FeaturesConfig struct {
	LuaScripting bool `yaml:"lua_scripting"`
	PubSub       bool `yaml:"pub_sub"`
	ComplexTypes bool `yaml:"complex_types"`
	Transactions bool `yaml:"transactions"`
	GeoCommands  bool `yaml:"geo_commands"`
	Streams      bool `yaml:"streams"`
}

var GlobalConfig *Config

func LoadConfig(filename string) (*Config, error) {
	// Default config
	config := &Config{
		Server: ServerConfig{
			Mode: "rest",
			TCP: TCPConfig{
				Host:           "0.0.0.0",
				Port:           6380,
				MaxConnections: 1000,
				ReadTimeout:    30 * time.Second,
				WriteTimeout:   30 * time.Second,
			},
			REST: RESTConfig{
				Host:           "0.0.0.0",
				Port:           8080,
				CORSEnabled:    true,
				CORSOrigins:    []string{"*"},
				RequestTimeout: 30 * time.Second,
				MaxRequestSize: "10MB",
			},
		},
		Cache: CacheConfig{
			MaxMemory:         "1GB",
			MaxKeys:           1000000,
			DefaultTTL:        0,
			MaxTTL:            24 * time.Hour,
			CleanupInterval:   time.Minute,
			EvictionPolicy:    "lru",
			EvictionThreshold: 0.8,
		},
		Persistence: PersistenceConfig{
			Enabled:     true,
			File:        "./data/boltcache.json",
			Interval:    30 * time.Second,
			Compression: true,
			BackupCount: 3,
		},
		Features: FeaturesConfig{
			LuaScripting: true,
			PubSub:       true,
			ComplexTypes: true,
		},
	}

	// Load from file if exists
	if filename != "" {
		data, err := os.ReadFile(filename)
		if err != nil {
			if !os.IsNotExist(err) {
				return nil, fmt.Errorf("failed to read config file: %v", err)
			}
			// File doesn't exist, use defaults
		} else {
			if err := yaml.Unmarshal(data, config); err != nil {
				return nil, fmt.Errorf("failed to parse config file: %v", err)
			}
		}
	}

	GlobalConfig = config
	return config, nil
}

func (c *Config) Validate() error {
	// Validate server mode
	if c.Server.Mode != "tcp" && c.Server.Mode != "rest" && c.Server.Mode != "both" {
		return fmt.Errorf("invalid server mode: %s", c.Server.Mode)
	}

	// Validate ports
	if c.Server.TCP.Port < 1 || c.Server.TCP.Port > 65535 {
		return fmt.Errorf("invalid TCP port: %d", c.Server.TCP.Port)
	}
	if c.Server.REST.Port < 1 || c.Server.REST.Port > 65535 {
		return fmt.Errorf("invalid REST port: %d", c.Server.REST.Port)
	}

	// Validate eviction policy
	validPolicies := map[string]bool{"lru": true, "lfu": true, "random": true, "ttl": true}
	if !validPolicies[c.Cache.EvictionPolicy] {
		return fmt.Errorf("invalid eviction policy: %s", c.Cache.EvictionPolicy)
	}

	// Validate thresholds
	if c.Cache.EvictionThreshold < 0.1 || c.Cache.EvictionThreshold > 1.0 {
		return fmt.Errorf("eviction threshold must be between 0.1 and 1.0")
	}

	return nil
}

func (c *Config) GetTCPAddress() string {
	return fmt.Sprintf("%s:%d", c.Server.TCP.Host, c.Server.TCP.Port)
}

func (c *Config) GetRESTAddress() string {
	return fmt.Sprintf("%s:%d", c.Server.REST.Host, c.Server.REST.Port)
}

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

// Helper function to parse memory size
func ParseMemorySize(size string) (int64, error) {
	if size == "" {
		return 0, nil
	}
	
	multiplier := int64(1)
	unit := size[len(size)-2:]
	
	switch unit {
	case "KB":
		multiplier = 1024
		size = size[:len(size)-2]
	case "MB":
		multiplier = 1024 * 1024
		size = size[:len(size)-2]
	case "GB":
		multiplier = 1024 * 1024 * 1024
		size = size[:len(size)-2]
	default:
		// Assume bytes
		if size[len(size)-1:] == "B" {
			size = size[:len(size)-1]
		}
	}
	
	var value int64
	fmt.Sscanf(size, "%d", &value)
	return value * multiplier, nil
}