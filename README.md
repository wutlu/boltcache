# BoltCache 🚀

**High-performance, Redis-compatible in-memory cache with RESTful API**

BoltCache is a modern, fast, and scalable in-memory cache system built in Go. It provides Redis-like functionality with better performance, RESTful API support, and enterprise-grade features.

[![Go Version](https://img.shields.io/badge/Go-1.21+-blue.svg)](https://golang.org)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)
[![API](https://img.shields.io/badge/API-REST-orange.svg)](README-REST.md)
[![PRs Welcome](https://img.shields.io/badge/PRs-welcome-brightgreen.svg)](CONTRIBUTING.md)

> **Keywords**: `redis`, `cache`, `in-memory`, `golang`, `rest-api`, `pub-sub`, `high-performance`, `microservices`, `docker`, `kubernetes`

## ✨ Features

- ⚡ **High Performance**: Ultra-low latency (0.01ms) with Go's concurrency
- 🌐 **Dual Protocol**: Modern HTTP/JSON REST API + Redis-compatible TCP protocol
- 🔄 **Pub/Sub Messaging**: Real-time messaging with WebSocket support
- ⏰ **TTL Support**: Automatic key expiration and cleanup
- 🔒 **Thread-Safe**: Concurrent operations with lock-free data structures
- 💾 **Persistence**: JSON-based disk storage with backup support
- 🔗 **Clustering**: Master-slave replication and high availability
- 📊 **Complex Data Types**: String, List, Set, Hash support
- 🔧 **Lua Scripting**: Execute custom scripts for complex operations
- 🛡️ **Security**: Token-based authentication and rate limiting
- 📈 **Monitoring**: Built-in metrics, health checks, and profiling
- ⚙️ **Configuration**: YAML-based configuration management

## 🚀 Quick Start

### Prerequisites

- Go 1.21 or higher
- Git

### Installation

```bash
# Clone the repository
git clone https://github.com/wutlu/boltcache.git
cd boltcache

# Install dependencies
go mod download

# Generate default configuration
make generate-config

# Start the server
make run-dev
```

The server will start on:
- **REST API**: http://localhost:8090
- **TCP Server**: localhost:6380 (Redis-compatible protocol)

### Quick Test

```bash
# Health check
curl http://localhost:8090/ping

# Set a value
curl -X PUT http://localhost:8090/cache/hello \
  -H "Content-Type: application/json" \
  -d '{"value": "world"}'

# Get the value
curl http://localhost:8090/cache/hello

# Test TCP protocol
telnet localhost 6380
SET mykey myvalue
GET mykey
PING
```

### Benchmark Testing

```bash
# Test BoltCache TCP performance
go run benchmark.go

# Compare with Redis
redis-benchmark -h localhost -p 6379 -t set,get -n 10000 -c 50
```

## 📖 Usage

### REST API

BoltCache provides a comprehensive RESTful API:

#### String Operations
```bash
# Set value
PUT /cache/{key}
{"value": "data", "ttl": "5m"}

# Get value
GET /cache/{key}

# Delete key
DELETE /cache/{key}
```

#### List Operations
```bash
# Push to list
POST /list/{key}
["item1", "item2"]

# Pop from list
DELETE /list/{key}
```

#### Set Operations
```bash
# Add to set
POST /set/{key}
["member1", "member2"]

# Get set members
GET /set/{key}
```

#### Hash Operations
```bash
# Set hash field
PUT /hash/{key}/{field}
{"value": "data"}

# Get hash field
GET /hash/{key}/{field}
```

#### Pub/Sub Operations
```bash
# Publish message
POST /publish/{channel}
{"message": "Hello World"}

# Subscribe (WebSocket)
GET /subscribe/{channel}
```

#### Lua Scripting
```bash
# Execute script
POST /eval
{
  "script": "redis.call('SET', KEYS[1], ARGV[1])",
  "keys": ["mykey"],
  "args": ["myvalue"]
}
```

### TCP Protocol

BoltCache supports high-performance TCP connections with Redis-compatible commands:

```bash
# Connect via telnet
telnet localhost 6380

# Redis-compatible commands
SET mykey myvalue
GET mykey
LPUSH mylist item1 item2
LPOP mylist
SADD myset member1
HSET myhash field value
PING
```

**TCP Performance:**
- 67K+ SET operations per second
- 71K+ GET operations per second
- Ultra-low latency: 0.01ms average
- Full Redis command compatibility

### Configuration

BoltCache uses YAML configuration files:

```yaml
# config.yaml
server:
  mode: "rest"  # tcp, rest, both
  rest:
    port: 8090
    cors_enabled: true
  tcp:
    port: 6380

cache:
  max_memory: "1GB"
  cleanup_interval: "1m"
  eviction_policy: "lru"

persistence:
  enabled: true
  file: "./data/cache.json"
  interval: "30s"

security:
  auth:
    enabled: true
    tokens: ["your-secret-token"]
```

### Environment-Specific Configs

```bash
# Development
make run-dev

# Production
make run-prod

# Custom config
go run main-config.go -config custom.yaml
```

## 🔧 Development

### Available Commands

```bash
# Server operations
make run              # Run with default config
make run-dev          # Run development mode
make run-prod         # Run production mode
make build            # Build binary

# Configuration
make generate-config  # Generate default config
make validate-config  # Validate configuration
make show-config      # Show current config

# Testing
make test-rest        # Test REST API
make test-auth        # Test authentication
make test-pubsub      # Test Pub/Sub messaging
# Web client: http://localhost:8090/rest-client.html

# Clustering
make cluster-master   # Start cluster master
make cluster-slave    # Start cluster slave
```

### Testing Tools

1. **Web Client**: Interactive browser-based client
   ```bash
   # Start server
   make run-dev
   
   # Open web client
   open http://localhost:8090/rest-client.html
   ```

2. **Postman Collection**: Import `BoltCache.postman_collection.json`

3. **cURL Scripts**: 
   ```bash
   ./rest-examples.sh
   ./auth-examples.sh
   ./test-pubsub.sh
   ```

4. **Interactive Client**:
   ```bash
   go run client.go interactive
   ```

## 🛡️ Security

### Authentication

BoltCache supports token-based authentication:

```bash
# Using Authorization header
curl -H "Authorization: Bearer your-token" \
  http://localhost:8090/cache/key

# Using X-API-Token header
curl -H "X-API-Token: your-token" \
  http://localhost:8090/cache/key

# Using query parameter
curl "http://localhost:8090/cache/key?token=your-token"
```

### Configuration

```yaml
security:
  auth:
    enabled: true
    method: "token"
    tokens:
      - "production-token-1"
      - "production-token-2"
  rate_limit:
    enabled: true
    requests_per_second: 1000
    burst: 100
```

## 📊 Performance

### TCP Protocol Benchmarks

BoltCache vs Redis (TCP Protocol):

| Operation | BoltCache TCP | Redis TCP | Comparison |
|-----------|---------------|-----------|------------|
| SET       | 67,803 ops/s  | 84,746 ops/s | Redis +25% |
| GET       | 71,530 ops/s  | 102,041 ops/s| Redis +43% |
| SET Latency| 0.01 ms      | 0.279 ms  | BoltCache -96% |
| GET Latency| 0.01 ms      | 0.271 ms  | BoltCache -96% |
| Memory    | 45MB          | 67MB      | BoltCache -33% |

**Key Insights:**
- **Latency**: BoltCache excels with 96% lower latency
- **Throughput**: Redis leads in operations per second
- **Memory**: BoltCache uses 33% less memory
- **Use Case**: Choose BoltCache for low-latency, Redis for high-throughput

*Benchmarks: MacBook Pro M1, 10K ops, 50 concurrent connections*

### Performance Tuning

```yaml
performance:
  max_goroutines: 10000
  gc_percent: 100
  connection_pool:
    max_idle: 100
    max_active: 1000
```

## 🏗️ Architecture

### Core Components

- **Storage Engine**: Lock-free `sync.Map` for concurrent access
- **Network Layer**: HTTP/REST and TCP protocol support
- **Pub/Sub System**: Channel-based non-blocking messaging
- **Persistence Layer**: JSON-based storage with compression
- **Authentication**: Token-based security middleware
- **Configuration**: YAML-based configuration management

### Data Flow

```
Client Request → Auth Middleware → Router → Cache Engine → Storage
                                      ↓
Pub/Sub System ← Background Tasks ← Persistence Layer
```

## 🐳 Docker Deployment

### Single Node

```bash
# Build image
docker build -t boltcache .

# Run container
docker run -p 8090:8090 -v ./data:/app/data boltcache
```

### Docker Compose

```bash
# Start cluster
docker-compose up
```

### Kubernetes

```bash
# Deploy to Kubernetes
kubectl apply -f k8s/
```

## 📚 Documentation

- [API Reference](README-REST.md)
- [Configuration Guide](README-CONFIG.md)
- [Authentication Guide](README-AUTH.md)
- [Postman Collection](BoltCache.postman_collection.json)

## 🤝 Contributing

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## 📄 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## 🙏 Acknowledgments

- Inspired by Redis architecture
- Built with Go's excellent concurrency primitives
- Uses Gorilla WebSocket and Mux libraries

## 📞 Support

- 🐛 Issues: [GitHub Issues](https://github.com/wutlu/boltcache/issues)
- 📧 Email: mutlu@etsetra.com
- 🌐 Website: [etsetra.com](https://etsetra.com)
- 💼 LinkedIn: [Alper Mutlu Toksöz](https://linkedin.com/in/wutlu)

---

**Created by [Alper Mutlu Toksöz](https://github.com/wutlu) with ❤️**