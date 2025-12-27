package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

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
			CleanupWhenExceeds: 20,
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
