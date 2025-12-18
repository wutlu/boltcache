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
   git clone https://github.com/wutlu/boltcache.git
   ```
3. **Create** a feature branch:
   ```bash
   git checkout -b feature/amazing-feature
   ```
4. **Make** your changes
5. **Test** your changes:
   ```bash
   make test-rest
   make test-auth
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

```bash
# Install dependencies
go mod download

# Run development server
make run-dev

# Run tests
make test-rest
make test-auth

# Validate configuration
make validate-config
```

## Code Style

- Follow Go conventions and `gofmt`
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