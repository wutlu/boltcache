# Contributing to BoltCache

Thank you for your interest in contributing to BoltCache! 🚀

## How to Contribute

### 🐛 Reporting Bugs

1. Check if the bug has already been reported in [Issues](https://github.com/wutlu/boltcache/issues)
2. Create a new issue with:
   - Clear description of the problem
   - Steps to reproduce
   - Expected vs actual behavior
   - System information (OS, Go version)

### 💡 Suggesting Features

1. Check existing [Issues](https://github.com/wutlu/boltcache/issues) for similar requests
2. Create a new issue with:
   - Clear feature description
   - Use case and benefits
   - Possible implementation approach

### 🔧 Code Contributions

1. **Fork** the repository
2. **Clone** your fork:
   ```bash
   git clone https://github.com/<your-user>/boltcache.git
   ```
3. **Create** a feature branch:
   ```bash
   git checkout -b feature/amazing-feature
   ```
4. **Make** your changes
5. **Test** your changes:
   ```bash
   make test-race
   make lint
   ```
6. **Commit** with clear messages:
   ```bash
   git commit -m "Add amazing feature"
   ```
7. **Push** to your fork:
   ```bash
   git push origin feature/amazing-feature
   ```
8. **Create** a Pull Request

## Development Setup

Requires Go 1.26 or newer.

```bash
make help            # list all targets
make run-dev         # dev server: auth on (token dev-token-123), debug logs
make test-race       # unit + integration tests with the race detector
make lint            # golangci-lint
make fuzz            # fuzz the RESP parser and glob matcher
make bench           # micro benchmarks
make benchmark       # load test a running server
```

### Project layout

| Path | Contents |
|------|----------|
| `internal/cache` | Sharded keyspace, value types, expiry, eviction, snapshots |
| `internal/command` | Redis command implementations, transactions, Lua scripting |
| `internal/resp` | RESP parser/encoder |
| `internal/server` | gnet listeners (RESP + text), REST API, WebSocket, metrics |
| `internal/pubsub`, `internal/glob`, `internal/auth` | Pub/sub hub, glob matching, tokens |
| `config` | Configuration loading and validation |
| `cmd` | CLI (`server`, `client`, `config`, `version`) |

New commands go in `internal/command` (register them in the matching `register*` function) and need a test in `engine_test.go`; behaviour should match Redis. The integration tests in `internal/server` start real servers and use go-redis, raw TCP and WebSocket clients.

## Code Style

- Follow Go conventions; `make lint` must pass
- Add comments for public functions
- Write tests for new features
- Keep functions small and focused
- Use meaningful variable names

## Pull Request Guidelines

- **Title**: Clear and descriptive
- **Description**: What changes and why
- **Tests**: Include tests for new features
- **Documentation**: Update README if needed
- **Breaking Changes**: Clearly marked

## Areas for Contribution

- 🚀 Performance optimizations
- 🔒 Security enhancements
- 📚 Documentation improvements
- 🧪 Test coverage
- 🐳 Docker/K8s improvements
- 🌐 New API endpoints
- 🔧 Configuration options

## Questions?

- 📧 Email: mutlu@etsetra.com
- 🐛 Issues: [GitHub Issues](https://github.com/wutlu/boltcache/issues)

Thank you for making BoltCache better! ❤️