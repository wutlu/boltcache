package config

import (
	"time"
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
	Mode      string     `yaml:"mode"`
	SwaggerUI bool       `yaml:"swaggerui,omitempty"`
	TCP       TCPConfig  `yaml:"tcp"`
	REST      RESTConfig `yaml:"rest"`
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

	// Cleanup starts only when the total number of backup files
	// exceeds this value.
	// Old backups are deleted until BackupCount files remain.
	CleanupWhenExceeds int `yaml:"cleanup_when_exceeds"`
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
	MaxGoroutines   int                  `yaml:"max_goroutines"`
	ReadBufferSize  string               `yaml:"read_buffer_size"`
	WriteBufferSize string               `yaml:"write_buffer_size"`
	GCPercent       int                  `yaml:"gc_percent"`
	ConnectionPool  ConnectionPoolConfig `yaml:"connection_pool"`
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
