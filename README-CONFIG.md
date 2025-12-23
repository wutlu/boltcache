# BoltCache Configuration Guide 🔧

Comprehensive configuration management for BoltCache server.

## Quick Start

```bash
# Generate default config
make generate-config

# Run with default config
make run

# Run development mode
make run-dev

# Run production mode  
make run-prod

# Validate configuration
make validate-config
```

## Configuration Files

### Default Configs
- `config.yaml` - Default configuration
- `config-dev.yaml` - Development settings
- `config-prod.yaml` - Production settings

### Custom Config
```bash
# Use custom config file
go run . server --config /path/to/custom.yaml
```

## Configuration Sections

### 🖥️ Server Configuration
```yaml
server:
  mode: "rest"  # tcp, rest, both
  swaggerui: true # Swagger UI for API exploration
  tcp:
    host: "0.0.0.0"
    port: 6380
    max_connections: 1000
    read_timeout: "30s"
    write_timeout: "30s"
  rest:
    host: "0.0.0.0"
    port: 8080
    cors_enabled: true
    cors_origins: ["*"]
    request_timeout: "30s"
    max_request_size: "10MB"
```

### 💾 Cache Configuration
```yaml
cache:
  max_memory: "1GB"        # Memory limit
  max_keys: 1000000        # Key count limit
  default_ttl: "0s"        # Default expiration
  max_ttl: "24h"           # Maximum TTL allowed
  cleanup_interval: "1m"   # Cleanup frequency
  eviction_policy: "lru"   # lru, lfu, random, ttl
  eviction_threshold: 0.8  # Start evicting at 80%
```

### 💿 Persistence Configuration
```yaml
persistence:
  enabled: true
  file: "./data/boltcache.json"
  interval: "30s"         # Save frequency
  compression: true       # Compress data
  backup_count: 3         # Keep N backups
  snapshot:
    enabled: true
    interval: "5m"
    directory: "./snapshots"
```

### 🔒 Security Configuration
```yaml
security:
  auth:
    enabled: false
    method: "token"       # token, basic, jwt
    tokens: ["secret-token"]
  tls:
    enabled: false
    cert_file: "cert.pem"
    key_file: "key.pem"
  rate_limit:
    enabled: true
    requests_per_second: 1000
    burst: 100
```

### 📊 Monitoring Configuration
```yaml
monitoring:
  metrics:
    enabled: true
    endpoint: "/metrics"
    interval: "10s"
  health:
    enabled: true
    endpoint: "/health"
  profiling:
    enabled: false
    endpoint: "/debug/pprof"
```

### ⚡ Performance Configuration
```yaml
performance:
  max_goroutines: 10000
  read_buffer_size: "4KB"
  write_buffer_size: "4KB"
  gc_percent: 100
  connection_pool:
    max_idle: 100
    max_active: 1000
    idle_timeout: "5m"
```

### 🎛️ Feature Flags
```yaml
features:
  lua_scripting: true
  pub_sub: true
  complex_types: true
  transactions: false
  geo_commands: false
  streams: false
```

## Environment-Specific Configs

### Development
```yaml
# config-dev.yaml
server:
  mode: "both"  # Both TCP and REST
cache:
  max_memory: "512MB"
logging:
  level: "debug"
  format: "text"
```

### Production
```yaml
# config-prod.yaml
server:
  mode: "rest"
cache:
  max_memory: "4GB"
  eviction_threshold: 0.85
security:
  auth:
    enabled: true
  rate_limit:
    requests_per_second: 10000
logging:
  level: "info"
  format: "json"
```

## Memory Size Format

Supported units:
- `B` - Bytes
- `KB` - Kilobytes  
- `MB` - Megabytes
- `GB` - Gigabytes

Examples:
```yaml
max_memory: "1GB"
max_request_size: "10MB"
read_buffer_size: "4KB"
```

## Time Duration Format

Supported units:
- `s` - Seconds
- `m` - Minutes
- `h` - Hours

Examples:
```yaml
cleanup_interval: "1m"
max_ttl: "24h"
request_timeout: "30s"
```

## Configuration Validation

```bash
# Validate current config
make validate-config

# Validate specific config
go run . config validate --config config-prod.yaml
```

## Environment Variables

Override config with environment variables:
```bash
export BOLTCACHE_SERVER_REST_PORT=9090
export BOLTCACHE_CACHE_MAX_MEMORY=2GB
export BOLTCACHE_LOGGING_LEVEL=debug
```

## Docker Configuration

```dockerfile
# Mount config file
docker run -v ./config-prod.yaml:/app/config.yaml boltcache -config config.yaml

# Use environment variables
docker run -e BOLTCACHE_SERVER_REST_PORT=8080 boltcache
```

## Best Practices

### Development
- Enable debug logging
- Use both TCP and REST modes
- Frequent persistence saves
- Lower memory limits

### Production
- JSON logging format
- Authentication enabled
- Rate limiting configured
- Backup and snapshots enabled
- Performance tuning applied

### Security
- Disable CORS in production
- Use TLS certificates
- Enable authentication
- Configure rate limiting
- Restrict CORS origins

Bu configuration sistemi ile BoltCache'i her ortam için optimize edebilirsin! 🎯