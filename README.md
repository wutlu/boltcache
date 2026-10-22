# BoltCache ⚡

**A Redis-compatible in-memory cache with a built-in REST/WebSocket API, written in Go.**

BoltCache speaks the Redis protocol (RESP), so `redis-cli`, `redis-benchmark` and client libraries such as go-redis, ioredis and redis-py work out of the box. The same data is also available over a JSON REST API with WebSocket pub/sub, so browsers, serverless functions and shell scripts can use it without a Redis client or a proxy. One static binary, no dependencies.

> **Project status: beta.** 2.x is a rewrite with an extensive test suite, but it hasn't been battle-tested in production yet and has no replication. Read [When to use BoltCache](#-when-to-use-boltcache) and [How this project is built](#-how-this-project-is-built) before relying on it.

[![CI](https://github.com/wutlu/boltcache/actions/workflows/ci.yml/badge.svg)](https://github.com/wutlu/boltcache/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/wutlu/boltcache)](https://github.com/wutlu/boltcache/releases)
[![Go Reference](https://pkg.go.dev/badge/github.com/wutlu/boltcache/v2.svg)](https://pkg.go.dev/github.com/wutlu/boltcache/v2)
[![Go Version](https://img.shields.io/github/go-mod/go-version/wutlu/boltcache)](go.mod)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

> **Keywords**: `redis`, `resp`, `cache`, `in-memory`, `golang`, `rest-api`, `websocket`, `pub-sub`, `lua`, `docker`

## ✨ Features

- 🔌 **Redis compatible**: 138 commands over RESP2 for strings, lists, sets, sorted sets, hashes, keys/TTL, `SCAN`, pub/sub, `MULTI`/`EXEC`/`WATCH`, Lua and server commands
- ⚡ **Multi-core**: sharded keyspace and an event-driven network layer ([gnet](https://github.com/panjf2000/gnet)); see the [reproducible benchmarks](BENCHMARKS.md)
- 🌐 **REST API**: JSON over HTTP, WebSocket subscriptions, a generic `/command` endpoint, OpenAPI docs at `/docs` and a built-in web client
- 🔧 **Real Lua scripting**: `EVAL`/`EVALSHA`/`SCRIPT` with `redis.call`/`redis.pcall`; scripts run atomically on their keys, in a sandbox, with a timeout
- 🔒 **Atomic by design**: every command is atomic, and multi-key commands, transactions and scripts lock all their keys
- 🧹 **Memory management**: `max_memory`/`max_keys` limits with `lru`, `lfu`, `random`, `ttl` or `noeviction` policies
- 💾 **Persistence**: typed, crash-safe snapshots (atomic rename + fsync), optional gzip, rotating backups, restored on startup
- 🛡️ **Security**: token auth on every protocol (REST, RESP, TCP), CORS allow-list, per-IP rate limiting, TLS for HTTP, request size limits
- 📈 **Observability**: Prometheus `/metrics`, `/health`, `INFO`, structured JSON logs, optional pprof
- 🐳 **Cloud native**: multi-arch distroless Docker image, configuration through `${ENV}` variables, graceful shutdown

## 🚀 Quick Start

### Docker

```bash
docker run -d --name boltcache -p 6382:6382 -p 8090:8090 -v boltcache-data:/data ghcr.io/wutlu/boltcache:latest

redis-cli -p 6382 SET greeting "hello world" EX 60
curl localhost:8090/cache/greeting
```

Enable authentication with `-e BOLTCACHE_AUTH=true -e BOLTCACHE_TOKEN=change-me`. See [`docker-compose.yml`](docker-compose.yml).

### Binary

Download a binary for Linux, macOS, Windows or FreeBSD from [Releases](https://github.com/wutlu/boltcache/releases), or install with Go 1.26+:

```bash
go install github.com/wutlu/boltcache/v2@latest
boltcache config generate   # writes a documented config.yaml
boltcache server
```

### From source

```bash
git clone https://github.com/wutlu/boltcache.git
cd boltcache
make run          # or: make run-dev (auth enabled, token dev-token-123)
```

By default the server listens on:

| Port | Protocol | Use it with |
|------|----------|-------------|
| `6382` | RESP (Redis protocol) | `redis-cli -p 6382`, go-redis, ioredis, redis-py, `redis-benchmark` |
| `6380` | Text protocol + RESP | `telnet`/`nc` (`SET k v` → `OK`), BoltCache 1.x clients, and Redis clients |
| `8090` | HTTP | REST API, WebSocket pub/sub, `/metrics`, `/docs`, web client at `/` |

## 📖 Usage

### Redis clients

```bash
redis-cli -p 6382
127.0.0.1:6382> HSET user:1 name Ada lang Go
(integer) 2
127.0.0.1:6382> EXPIRE user:1 3600
(integer) 1
127.0.0.1:6382> EVAL "return redis.call('HGET', KEYS[1], 'name')" 1 user:1
"Ada"
```

```go
rdb := redis.NewClient(&redis.Options{Addr: "localhost:6382"})
rdb.Set(ctx, "key", "value", time.Minute)
```

<details>
<summary><b>Supported commands</b></summary>

| Group | Commands |
|-------|----------|
| Strings | `GET` `SET` (EX/PX/EXAT/PXAT/NX/XX/KEEPTTL/GET) `SETNX` `SETEX` `PSETEX` `GETSET` `GETDEL` `GETEX` `MGET` `MSET` `MSETNX` `INCR` `DECR` `INCRBY` `DECRBY` `INCRBYFLOAT` `APPEND` `STRLEN` `GETRANGE` `SUBSTR` |
| Keys | `DEL` `UNLINK` `EXISTS` `TOUCH` `TYPE` `EXPIRE` `PEXPIRE` `EXPIREAT` `PEXPIREAT` (NX/XX/GT/LT) `TTL` `PTTL` `EXPIRETIME` `PEXPIRETIME` `PERSIST` `KEYS` `SCAN` (MATCH/COUNT/TYPE) `RANDOMKEY` `RENAME` `RENAMENX` `DBSIZE` `FLUSHALL` `FLUSHDB` |
| Lists | `LPUSH` `RPUSH` `LPUSHX` `RPUSHX` `LPOP` `RPOP` (with count) `LLEN` `LRANGE` `LINDEX` `LSET` `LTRIM` `LREM` `RPOPLPUSH` `LMOVE` |
| Sets | `SADD` `SREM` `SMEMBERS` `SISMEMBER` `SMISMEMBER` `SCARD` `SPOP` `SRANDMEMBER` `SINTER` `SUNION` `SDIFF` `SINTERSTORE` `SUNIONSTORE` `SDIFFSTORE` `SMOVE` `SSCAN` |
| Hashes | `HSET` `HMSET` `HSETNX` `HGET` `HMGET` `HGETALL` `HDEL` `HEXISTS` `HLEN` `HKEYS` `HVALS` `HSTRLEN` `HINCRBY` `HINCRBYFLOAT` `HSCAN` |
| Sorted sets | `ZADD` (NX/XX/GT/LT/CH/INCR) `ZINCRBY` `ZSCORE` `ZMSCORE` `ZCARD` `ZCOUNT` `ZRANK` `ZREVRANK` `ZREM` `ZRANGE` (BYSCORE/REV/LIMIT/WITHSCORES) `ZREVRANGE` `ZRANGEBYSCORE` `ZREVRANGEBYSCORE` `ZPOPMIN` `ZPOPMAX` `ZREMRANGEBYRANK` `ZREMRANGEBYSCORE` `ZSCAN` |
| Pub/Sub | `SUBSCRIBE` `PSUBSCRIBE` `UNSUBSCRIBE` `PUNSUBSCRIBE` `PUBLISH` `PUBSUB` (CHANNELS/NUMSUB/NUMPAT) |
| Transactions | `MULTI` `EXEC` `DISCARD` `WATCH` `UNWATCH` |
| Scripting | `EVAL` `EVALSHA` `EVAL_RO` `EVALSHA_RO` `SCRIPT` (LOAD/EXISTS/FLUSH) |
| Server | `PING` `ECHO` `AUTH` `HELLO` `QUIT` `RESET` `SELECT` `TIME` `INFO` `CONFIG GET` `CLIENT` `COMMAND` `SAVE` `BGSAVE` `LASTSAVE` `MEMORY` `WAIT` |

Not supported yet: lexicographic sorted set ranges (`ZRANGEBYLEX`, `BYLEX`), sorted set algebra (`ZUNIONSTORE`, `ZINTER`...), streams, bitmaps, HyperLogLog, geo commands, blocking commands (`BLPOP`...), RESP3, ACL users and multiple databases. Unknown commands return an error rather than being ignored.
</details>

### REST API

```bash
# Strings (with optional TTL)
curl -X PUT localhost:8090/cache/session:42 -d '{"value": "active", "ttl": "30m"}'
curl localhost:8090/cache/session:42
curl localhost:8090/cache/session:42/ttl

# Lists, sets, hashes
curl -X POST localhost:8090/list/jobs -d '["job1", "job2"]'
curl -X POST localhost:8090/set/tags -d '["go", "cache"]'
curl -X PUT  localhost:8090/hash/user:1/name -d '{"value": "Ada"}'
curl localhost:8090/hash/user:1            # {"value": {"name": "Ada"}}

# Any Redis command
curl -X POST localhost:8090/command -d '{"args": ["INCRBY", "visits", "5"]}'

# Pub/Sub: WebSocket subscribers receive messages from REST and RESP publishers
websocat ws://localhost:8090/subscribe/news &
curl -X POST localhost:8090/publish/news -d '{"message": "hello"}'
```

Full reference: [README-REST.md](README-REST.md) and the interactive docs at `http://localhost:8090/docs`.

### Lua scripting

Scripts get `KEYS` and `ARGV` and call commands with `redis.call`/`redis.pcall`, using the same type conversions as Redis. A script holds the locks of the keys passed in `KEYS`, so it runs atomically; touching keys that weren't declared is an error (the same rule Redis Cluster has).

```bash
# A fixed-window rate limiter
redis-cli -p 6382 EVAL "
  local n = redis.call('INCR', KEYS[1])
  if n == 1 then redis.call('EXPIRE', KEYS[1], ARGV[1]) end
  return n" 1 rate:client42 60
```

Scripts run in a sandbox (no file access, no `load`), cannot leak globals and are stopped after `features.lua_timeout` (5s by default).

### Text protocol

Port 6380 keeps the line-based protocol of BoltCache 1.x for `telnet`/`nc` users and existing clients:

```
$ nc localhost 6380
SET user:1 John
OK
GET user:1
VALUE John
SET session abc 5m
OK
SMEMBERS tags
ARRAY go cache
```

## ⚙️ Configuration

All settings are optional; see the documented [config.yaml](config.yaml) and [README-CONFIG.md](README-CONFIG.md).

```yaml
cache:
  max_memory: "1GB"
  eviction_policy: "lru"     # lru, lfu, random, ttl, noeviction
persistence:
  enabled: true
  file: "./data/boltcache.json"
  interval: "30s"
security:
  auth:
    enabled: true
    tokens: ["${BOLTCACHE_TOKEN}"]   # read from the environment
```

```bash
boltcache config generate     # write a documented default config
boltcache config validate     # check a config file (reports every error)
boltcache config show         # print the effective configuration
boltcache server --config config-prod.yaml
```

## 🛡️ Security

When `security.auth.enabled` is true, every protocol requires a token:

```bash
curl -H "Authorization: Bearer $TOKEN" localhost:8090/cache/key   # or X-API-Token, or ?token=
redis-cli -p 6382 -a "$TOKEN" GET key                             # AUTH <token>
```

`/ping`, `/health`, `/ready`, `/docs` and the web client stay public; everything else, including `/metrics`, requires the token. Tokens are compared in constant time. See [README-AUTH.md](README-AUTH.md) and [SECURITY.md](SECURITY.md).

## 📈 Monitoring

- `GET /metrics`: Prometheus metrics (`boltcache_keys`, `boltcache_memory_used_bytes`, `boltcache_keyspace_hits_total`, `boltcache_evicted_keys_total`, `boltcache_commands_processed_total`...)
- `GET /health`, `GET /ready`: liveness/readiness probes
- `INFO` over RESP, `GET /info` over HTTP
- `logging.format: json` for structured logs; `monitoring.profiling.enabled` exposes pprof

## 📊 Performance

See [BENCHMARKS.md](BENCHMARKS.md) for the full results, method and caveats. Every number comes from [`bench/run.sh`](bench/run.sh), which runs `redis-benchmark` against BoltCache, Redis, Valkey and Dragonfly with the same pinned CPU budget, and runs in CI.

Summary of the latest run (GitHub Actions, 2 server cores):

- **Without pipelining**: BoltCache is in the top group with Dragonfly, at about half of Redis' median latency.
- **With pipeline 16**: BoltCache leads `LPUSH`/`HSET` and is 10–18% behind single-threaded Redis on `SET`/`GET`/`INCR`.
- **`ZADD` with pipelining** reaches only about 40% of Redis' throughput. This is a known weakness.

BoltCache 2.x is about twice as fast as 1.0. Measured on an M1 Pro with `boltcache client benchmark`, 50 clients: `SET` 78K → 147K ops/s and `GET` with pipeline 16 1.13M → 2.32M ops/s.

Measure on your own hardware:

```bash
bench/run.sh                                     # needs Docker; Linux recommended
SERVER_CPUS=0-7 CLIENT_CPUS=8-15 bench/run.sh    # bigger machines
boltcache client benchmark --addr localhost:6382 --pipeline 16   # quick check against a running server
```

## 🤔 When to use BoltCache

BoltCache is a good fit when you want:

- A cache or ephemeral store that **both backend services (Redis clients) and HTTP-only environments** can use: browsers, edge/serverless functions, shell scripts, low-code tools, IoT devices. A single binary serves RESP, REST and WebSocket pub/sub over the same data, with no extra proxy.
- A small, self-contained Redis-compatible server for development, CI, internal tools or single-node deployments, with Prometheus metrics and a web UI built in.
- Something you can read and extend: plain Go with no cgo, one command engine shared by every protocol, and straightforward data structures.

Look elsewhere when you need:

- **High availability or horizontal scaling**: BoltCache has no replication or clustering yet. Use Redis, [Valkey](https://valkey.io) or [Dragonfly](https://www.dragonflydb.io).
- **Full Redis compatibility**: streams, RESP3, ACLs, modules, client-side caching and the commands listed above as unsupported are missing.
- **Durable messaging**: pub/sub here is fire-and-forget, like Redis pub/sub. For persistent streams, at-least-once delivery or request/reply, use [NATS](https://nats.io) (JetStream) or Kafka.
- **Strongly consistent configuration or coordination** (leader election, locks across machines): use [etcd](https://etcd.io) or Consul, which replicate with Raft.
- **Data that must not be lost**: snapshots are periodic (every 30s by default), so a crash loses recent writes. There's no append-only file yet.

| | BoltCache | Redis / Valkey | Dragonfly | NATS | etcd |
|---|---|---|---|---|---|
| Primary use | Cache + HTTP API | Cache, data structures | Cache, data structures | Messaging, streams | Consistent config / coordination |
| Redis protocol | ✅ (subset) | ✅ | ✅ (most) | ❌ | ❌ |
| Built-in REST + WebSocket | ✅ | ❌ | ❌ | ❌ (WebSocket for NATS clients) | gRPC + JSON gateway |
| Multi-threaded | ✅ | I/O threads | ✅ | ✅ | ✅ |
| Replication / HA | ❌ (roadmap) | ✅ | ✅ | ✅ | ✅ (Raft) |
| Persistence | Snapshots | RDB + AOF | Snapshots | JetStream | Raft log |

## 🧪 How this project is built

BoltCache started as an AI-assisted ("vibe coded") experiment, and 1.0 shipped with claims it couldn't back up. The [first Hacker News thread](https://news.ycombinator.com/item?id=46348004) called that out, fairly. There were no tests, the performance numbers couldn't be reproduced, and the "Lua engine" understood a single hard-coded call.

2.x was rewritten, again with heavy use of AI coding tools (Claude). The difference is in how the result is verified:

- **Tests**: unit tests for every package, plus integration tests that start real servers and drive them with go-redis, raw TCP and WebSocket clients. The sorted set is checked against a naive reference model with randomized operations.
- **Race detector and fuzzing**: CI runs every test with `-race` on Linux and macOS and fuzzes the protocol parser and glob matcher on every push.
- **Independent review**: before 2.0 shipped, a separate review pass found 9 bugs, including quadratic parsing of large commands, a Lua sandbox escape between scripts and data loss in multi-step commands under memory pressure. All were fixed with regression tests.
- **Reproducible benchmarks**: [BENCHMARKS.md](BENCHMARKS.md) lists every number with the script that produced it. The script runs in CI with pinned CPUs against Redis, Valkey and Dragonfly, including the cases where BoltCache loses.
- **Behaviour matches Redis** where it implements a command, with the same replies, errors and edge cases. A difference from Redis is a bug; please [report it](https://github.com/wutlu/boltcache/issues/new?template=bug_report.yml).

## 🏗️ Architecture

```
 redis-cli / go-redis ─┐                       ┌─────────────────────────────┐
 telnet / 1.x clients ─┼─ gnet event loops ─┐  │ command engine (138 cmds)   │
                       │  (RESP + text)     ├─▶│  auth · MULTI/EXEC · Lua    │──▶ sharded keyspace
 HTTP / WebSocket ─────┴─ net/http (REST) ──┘  └─────────────────────────────┘    1024 shards, RWMutex each
                                                         │                         typed values, TTL,
                                                  pub/sub hub                      LRU/LFU eviction
                                                                                        │
                                                                         snapshots (atomic, gzip, backups)
```

- **Keyspace**: 1024 shards, each with its own lock and counters, so cores don't contend. Every command runs under its key's shard lock, which makes single-key commands atomic. Multi-key commands, `MULTI`/`EXEC` and scripts lock their shards in a fixed order, so they're atomic and deadlock-free.
- **Network**: gnet event loops (one per core) parse pipelined RESP incrementally without copying; HTTP uses the standard library.
- **One engine**: RESP, the text protocol, REST and Lua all execute through the same command implementation, so behaviour is identical everywhere.
- **Eviction**: like Redis, approximated by sampling: candidates are compared by last access (LRU), a logarithmic access counter (LFU) or expiry time (TTL).

## ⬆️ Upgrading from 1.x

- Snapshots written by 1.x are converted automatically on first start; the next save uses the new typed format.
- Authentication is now **enforced** when `security.auth.enabled` is true. In 1.x it was configured but never checked.
- The Redis-compatible port (6382) now supports every command listed above instead of only `PING`/`SET`/`GET`. The separate gnet text port (6381) is gone: port 6380 is served by gnet now and understands both protocols.
- Writing a different type to an existing key returns `WRONGTYPE` (1.x silently replaced it), and commands follow Redis semantics, e.g. `HSET` returns the number of new fields.
- The `cluster` command was removed: it never replicated data. Replication is on the roadmap.
- The Go module path is now `github.com/wutlu/boltcache/v2`, and Go 1.26+ is required to build.

See [CHANGELOG.md](CHANGELOG.md) for everything that changed.

## 🗺️ Roadmap

- Replication (primary/replica) and clustering
- Append-only file persistence
- Lexicographic sorted set ranges, streams and blocking list commands
- RESP3, ACL users and TLS for the RESP listener

## 🔧 Development

```bash
make help          # list targets
make test-race     # unit + integration tests with the race detector
make lint          # golangci-lint
make fuzz          # fuzz the protocol parser
make bench         # micro benchmarks
```

Integration tests start real servers and drive them with go-redis, raw TCP and WebSocket clients. See [CONTRIBUTING.md](CONTRIBUTING.md).

## 📚 Documentation

- [REST API](README-REST.md)
- [Configuration](README-CONFIG.md)
- [Authentication](README-AUTH.md)
- [Changelog](CHANGELOG.md)
- [Postman collection](BoltCache.postman_collection.json)

## 📄 License

MIT. See [LICENSE](LICENSE).

## 📞 Support

- 🐛 Issues: [GitHub Issues](https://github.com/wutlu/boltcache/issues)
- 📧 Email: alper@is.team
- 🌐 Website: [alper.vision](https://alper.vision)
- 💼 LinkedIn: [Alper Mutlu Toksöz](https://linkedin.com/in/alpervision)

---

**Created by [Alper Mutlu Toksöz](https://github.com/wutlu) with ❤️**
