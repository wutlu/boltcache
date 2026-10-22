# Security Policy

## Supported Versions

| Version | Supported          |
| ------- | ------------------ |
| 2.x     | :white_check_mark: |
| 1.x     | :x: (authentication was not enforced; please upgrade) |

## Reporting a Vulnerability

**Do not** open a public GitHub issue for security vulnerabilities. Instead, report them privately:

- Use GitHub's [private vulnerability reporting](https://github.com/wutlu/boltcache/security/advisories/new), or
- Email **mutlu@etsetra.com** with the subject "BoltCache Security Vulnerability".

Please include a description, steps to reproduce, the impact and, if you have one, a suggested fix.

### What to expect

- **Acknowledgment** within 72 hours
- **Initial assessment** within 7 days
- **Fix**: critical issues as soon as possible, others in the next release

Reporters are credited in the advisory unless they prefer otherwise.

## Hardening checklist

When running BoltCache outside a trusted network:

- Enable authentication (`security.auth.enabled: true`) with long random tokens injected through environment variables (`tokens: ["${BOLTCACHE_TOKEN}"]`).
- Restrict `server.rest.cors_origins` to your own origins.
- Enable `security.rate_limit` for the REST API.
- Serve the REST API over HTTPS (`security.tls`) or behind a TLS-terminating proxy.
- Keep the RESP (6382) and TCP (6380) ports on private networks: they don't support TLS yet.
- Set `cache.max_memory` so a client can't exhaust the host's memory.
- Keep `monitoring.profiling` disabled in production.
- Run the official Docker image (non-root, distroless) or an unprivileged user, and keep BoltCache up to date.
