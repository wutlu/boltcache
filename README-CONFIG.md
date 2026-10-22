# BoltCache Configuration 🔧

BoltCache reads a YAML file (default `config.yaml`, override with `--config`/`-c` or the `BOLTCACHE_CONFIG` environment variable). Every setting is optional: missing settings use the defaults below, and a missing file means "all defaults".

```bash
boltcache config generate            # write a documented config.yaml (--force to overwrite)
boltcache config validate -c my.yaml # report every problem at once
boltcache config show                # effective configuration, tokens masked
```

## Environment variables

`${VAR}` and `${VAR:-default}` anywhere in the file are replaced from the environment before parsing. Bare `$VAR` is left alone, so values containing `$` are safe.

```yaml
security:
  auth:
    enabled: ${BOLTCACHE_AUTH:-false}
    tokens: ["${BOLTCACHE_TOKEN}"]
cache:
  max_memory: "${BOLTCACHE_MAX_MEMORY:-1GB}"
```

## Reference

### server

| Setting | Default | Description |
|---------|---------|-------------|
| `mode` | `both` | `tcp` (TCP + RESP), `rest` (HTTP + RESP) or `both` |
| `swaggerui` | `false` | Serve `/docs` and `/openapi.json` |
| `tcp.host` / `tcp.port` | `0.0.0.0` / `6380` | Text protocol listener (also understands RESP) |
| `tcp.max_connections` | `10000` | Per-listener connection limit for TCP and RESP |
| `resp.enabled` | `true` | Run the Redis protocol listener |
| `resp.host` / `resp.port` | tcp host / `6382` | Redis protocol listener |
| `rest.host` / `rest.port` | `0.0.0.0` / `8090` | HTTP listener |
| `rest.cors_enabled` | `true` | Send CORS headers |
| `rest.cors_origins` | `["*"]` | Allowed origins (also checked for WebSocket upgrades) |
| `rest.request_timeout` | `30s` | Per-request timeout (not applied to WebSockets) |
| `rest.max_request_size` | `10MB` | Maximum request body |

### cache

| Setting | Default | Description |
|---------|---------|-------------|
| `max_memory` | `0` | Memory limit such as `512MB` or `2GB`; `0` = unlimited. Accounting is approximate (keys, values and per-item overhead, not Go runtime overhead). |
| `max_keys` | `0` | Key limit; `0` = unlimited |
| `eviction_policy` | `lru` | `lru`, `lfu`, `random`, `ttl` (nearest expiry first) or `noeviction` (writes fail with `OOM`) |
| `eviction_threshold` | `1.0` | Start evicting at this fraction of `max_memory` |
| `default_ttl` | `0s` | TTL applied by the REST API when a request doesn't set one |
| `max_ttl` | `0s` | Largest TTL the REST API accepts (`0` = no limit) |
| `cleanup_interval` | `1m` | How often expired keys are reclaimed. Expired keys are never returned in between. |

### persistence

| Setting | Default | Description |
|---------|---------|-------------|
| `enabled` | `true` | Save snapshots and restore them on startup |
| `file` | `./data/boltcache.json` | Snapshot path |
| `interval` | `30s` | Save interval; skipped when nothing changed. A final snapshot is written on shutdown. |
| `compression` | `false` | gzip snapshots (loading detects compression automatically) |
| `backup_count` | `3` | Previous snapshots kept as `<file>.backup.<timestamp>` |
| `cleanup_when_exceeds` | `0` | Prune backups only once this many exist (batches deletions) |

Snapshots are newline-delimited JSON with a header line and one typed entry per key; binary data is base64-encoded so it round-trips exactly. They're written to a temporary file, fsynced and atomically renamed, so a crash never leaves a truncated snapshot. Snapshots from BoltCache 1.x are converted on load. If a snapshot can't be read the server refuses to start rather than silently starting empty.

### security

| Setting | Default | Description |
|---------|---------|-------------|
| `auth.enabled` | `false` | Require a token on every protocol ([README-AUTH.md](README-AUTH.md)) |
| `auth.tokens` | `[]` | Accepted tokens (empty entries are ignored) |
| `tls.enabled` | `false` | Serve the REST API over HTTPS |
| `tls.cert_file` / `tls.key_file` | | PEM certificate and key |
| `rate_limit.enabled` | `false` | Per-client-IP token bucket for the REST API (`429` when exceeded) |
| `rate_limit.requests_per_second` / `burst` | `1000` / `100` | Bucket rate and size |

### logging

| Setting | Default | Description |
|---------|---------|-------------|
| `level` | `info` | `debug`, `info`, `warn`, `error` (debug logs every HTTP request) |
| `format` | `text` | `text` or `json` |
| `file` | | Log file (appended); empty = stderr. Use logrotate or your platform's log collection for rotation. |

### monitoring

| Setting | Default | Description |
|---------|---------|-------------|
| `metrics.enabled` / `endpoint` | `true` / `/metrics` | Prometheus metrics on the REST port |
| `health.enabled` / `endpoint` | `true` / `/health` | Health endpoint (plus `/ready`) |
| `profiling.enabled` / `endpoint` | `false` / `/debug/pprof` | Go pprof handlers (require a token when auth is enabled) |

### features and performance

| Setting | Default | Description |
|---------|---------|-------------|
| `features.lua_scripting` | `true` | Enable `EVAL`/`EVALSHA`/`SCRIPT` and `POST /eval` |
| `features.lua_timeout` | `5s` | Abort scripts running longer than this |
| `features.pub_sub` | `true` | Enable pub/sub commands and WebSockets |
| `performance.gc_percent` | `100` | Go GC target (`GOGC`) |

Settings from 1.x that were never implemented (`cluster.*`, `persistence.snapshot.*`, `performance.connection_pool.*`, `logging.max_size`...) are still accepted so old files keep loading, but they have no effect.

## Example files

- [config.yaml](config.yaml): every setting with its default, documented
- [config-dev.yaml](config-dev.yaml): local development, auth on (`dev-token-123`), debug logs, pprof
- [config-prod.yaml](config-prod.yaml): production starting point with auth, rate limiting, memory limit and JSON logs
- [deploy/docker/config.yaml](deploy/docker/config.yaml): the Docker image's config, driven by `BOLTCACHE_*` environment variables
