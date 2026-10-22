# Changelog

All notable changes to this project are documented here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses [Semantic Versioning](https://semver.org/).

## [2.0.1] - 2026-10-22

### Fixed

- `go install github.com/wutlu/boltcache/v2@latest` works: Go requires a `/v2` suffix in the module path of v2+ releases, so the module path is now `github.com/wutlu/boltcache/v2`.
- Release notes are included in GitHub releases again.

## [2.0.0] - 2026-10-22

BoltCache 2.0 is a ground-up rework of the engine, the network layer and the tooling. It is now a real Redis-compatible server: `redis-cli`, `redis-benchmark`, go-redis and other clients work against it, with real Lua scripting, transactions, eviction and crash-safe persistence. It is also about **2x faster** than 1.0 and outperforms Redis 8.4 on a multi-core machine in `redis-benchmark` (see the README for numbers and methodology).

### Highlights

- **Redis compatibility**: 120 commands over RESP2, up from `PING`/`SET`/`GET`. Strings with all `SET` options, keys/TTL (`EXPIRE` NX/XX/GT/LT, `SCAN` with MATCH/COUNT/TYPE), lists, sets, hashes, pub/sub with patterns, `MULTI`/`EXEC`/`WATCH`, Lua, `INFO`, `CONFIG GET`, `CLIENT`, `COMMAND`, `HELLO` and more.
- **Real Lua scripting** (gopher-lua) replaces the line-based imitation: `EVAL`, `EVALSHA`, `EVAL_RO`, `SCRIPT LOAD/EXISTS/FLUSH`, `redis.call`/`redis.pcall` with Redis type conversions. Scripts are atomic, sandboxed and time-limited.
- **Atomicity everywhere**: every command runs under its key's shard lock; multi-key commands, transactions and scripts lock all their keys in a fixed order, so they're atomic and can't deadlock.
- **Eviction and limits**: `cache.max_memory` and `cache.max_keys` are now enforced with `lru`, `lfu`, `random`, `ttl` or the new `noeviction` policy.
- **Crash-safe, typed persistence**: snapshots keep value types and binary data, are written atomically (temp file + fsync + rename), can be gzipped and are skipped when nothing changed. 1.x snapshots are converted automatically.
- **Security**: token authentication is enforced on REST, RESP and TCP; CORS allow-list (also for WebSockets) with CSRF protection, per-IP rate limiting, request size limits (strict ones for unauthenticated clients), HTTPS for the REST API, constant-time token checks, sandboxed Lua.
- **Observability**: Prometheus `/metrics`, `/health` and `/ready`, structured logging with `log/slog` (text/JSON), optional pprof.
- **Distribution**: prebuilt binaries for Linux, macOS, Windows and FreeBSD (amd64/arm64), multi-arch distroless images on `ghcr.io/wutlu/boltcache`, `go install github.com/wutlu/boltcache/v2@latest` (from 2.0.1).

### Added

- RESP listener (`server.resp`, port 6382) with incremental, zero-copy parsing: pipelining, partial reads and inline commands are handled correctly. The TCP port (6380) auto-detects RESP, so Redis clients work there too.
- REST endpoints: `GET /list/{key}`, `DELETE /set/{key}/{member}`, `GET /hash/{key}`, `DELETE /hash/{key}/{field}`, `GET /cache/{key}/ttl`, `GET /keys` (paginated), `POST /command` (run any command), `/health`, `/ready`, `/metrics`. `GET /cache/{key}` returns lists, sets and hashes too.
- Real token management: `POST/GET/DELETE /auth/tokens` create, list (masked, with usage statistics) and revoke runtime tokens valid for every protocol.
- `${VAR}` / `${VAR:-default}` environment expansion in configuration files and `BOLTCACHE_CONFIG`.
- `boltcache config show`, `boltcache version`, `boltcache config generate --force`; `config validate` reports every problem at once.
- `boltcache client benchmark`: parallel, pipelined load generator with p50/p99 latency. `client interactive` is now a RESP client similar to `redis-cli`, and `client test` runs a feature check.
- `features.lua_timeout`, `cache.default_ttl`/`max_ttl` for the REST API, `persistence.compression`, `security.rate_limit` and `security.tls` now take effect.
- Web client: "run any command" box, token stored in the browser, works on any host/port.
- CI on Linux and macOS with the race detector, fuzzing and golangci-lint; GoReleaser and Docker release automation; Dependabot.

### Fixed

- The Redis-compatible port hung on any command other than `PING`, `SET` and `GET` (no reply was sent), which also made `redis-benchmark` hang at startup.
- Commands split across TCP packets were silently dropped, and only the first three arguments of a command were parsed.
- On the high-performance text port, any command starting with `s` or `g` was treated as `SET`/`GET` (e.g. `SADD x y` stored a value).
- Authentication was configured but never enforced: the middleware was commented out, so tokens in `config-dev.yaml` protected nothing.
- Data races between hash/set/list writes, reads and snapshots could crash the server with `concurrent map read and map write`.
- After a restart, values written over RESP came back base64-encoded, and writing to a restored hash/set/list wiped its previous contents.
- Persistence wrote the snapshot in place, so a crash during a save could leave a truncated file.
- Pub/sub: dead subscribers were never removed, one slow subscriber blocked publishers, and concurrent writes to a WebSocket could panic the server.
- `POST /eval` panicked when Lua scripting was disabled; `ParseMemorySize("1")` panicked.
- The web client was vulnerable to XSS through cached values and pub/sub messages, and was hard-wired to `localhost:8090`.
- `KEYS`/`PSUBSCRIBE` glob matching could take exponential time on hostile patterns.
- `config validate` ignored `--config`; `config generate` overwrote existing files; `SIGTERM` didn't stop the HTTP server gracefully.

### Changed

- **Breaking**: the Go module path is `github.com/wutlu/boltcache/v2` (from 2.0.1) and Go 1.26 or newer is required to build.
- **Breaking**: authentication is enforced when `security.auth.enabled` is true (see above).
- **Breaking**: writing a different type to an existing key returns `WRONGTYPE` instead of silently replacing it, and replies follow Redis (e.g. `HSET` returns the number of new fields, `DEL` the number of deleted keys).
- **Breaking**: the separate gnet text listener on `tcp.port + 1` (6381) was removed; port 6380 now runs on gnet and serves both protocols.
- **Breaking**: the `cluster` command was removed. It started an isolated node and never replicated data; replication is on the roadmap.
- `gorilla/mux` was replaced with the standard library router; `gopkg.in/yaml.v3` (archived) with `go.yaml.in/yaml/v3`. All dependencies were updated.
- Defaults: `server.mode` is `both`, the REST port is 8090 everywhere, `eviction_threshold` is 1.0, snapshot compression is off, and `cors_origins` is empty (same origin only) instead of `*`, so web pages you visit can't use a local BoltCache. Non-GET requests from origins that aren't allowed get `403`.
- The Docker image is distroless, runs as non-root, is multi-arch and is configured through `BOLTCACHE_*` environment variables; `docker-compose.yml` was fixed.
- Swagger UI assets are pinned to a fixed version; the OpenAPI document covers every endpoint and declares the auth schemes.

## [1.0.0] - 2025-12-18

Initial release.

[2.0.1]: https://github.com/wutlu/boltcache/compare/v2.0.0...v2.0.1
[2.0.0]: https://github.com/wutlu/boltcache/compare/e6a00dc...v2.0.0
[1.0.0]: https://github.com/wutlu/boltcache/commit/f13537c
