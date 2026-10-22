# BoltCache REST API 🌐

The REST API exposes the same keyspace as the Redis protocol over JSON/HTTP, plus WebSocket pub/sub. Interactive documentation is served at `/docs` (OpenAPI document at `/openapi.json`) and a web client at `/`.

Base URL: `http://localhost:8090` (`server.rest.port`).

## Response format

Every endpoint returns the same envelope:

```json
{ "success": true, "value": "...", "count": 2, "error": "only on failure" }
```

| Status | Meaning |
|--------|---------|
| 200 | Success |
| 400 | Invalid request (bad JSON, invalid TTL, command error) |
| 401 | Missing or invalid token (when auth is enabled) |
| 404 | Key/field not found |
| 409 | The key holds a different type (`WRONGTYPE`) |
| 413/400 | Body larger than `server.rest.max_request_size` |
| 429 | Rate limit exceeded (`security.rate_limit`) |
| 507 | Memory limit reached with the `noeviction` policy |

## Authentication

When `security.auth.enabled` is true, send a token with any of:

```http
Authorization: Bearer <token>
X-API-Token: <token>
GET /cache/key?token=<token>        # for browser WebSockets
```

`/ping`, `/health`, `/ready`, `/`, `/docs` and `/openapi.json` never require a token. See [README-AUTH.md](README-AUTH.md).

## Endpoints

### Keys and strings

```http
PUT    /cache/{key}          {"value": "data", "ttl": "5m"}   # set (POST also accepted)
GET    /cache/{key}                                          # get a value of any type
DELETE /cache/{key}                                          # delete
GET    /cache/{key}/ttl                                      # {"ttl_ms": 299000, "ttl": "4m59s", "persistent": false}
GET    /keys?pattern=user:*&limit=1000&cursor=0              # paginated key listing
```

- `value` can be any JSON value; strings are stored as-is, other values as their JSON text.
- `ttl` is a Go duration (`30s`, `5m`, `2h`). Without it, `cache.default_ttl` applies; `cache.max_ttl` caps it.
- `GET /cache/{key}` returns strings as strings, lists and sets as arrays and hashes as objects.
- `/keys` uses `SCAN`, so it never blocks the server; pass the returned `cursor` to get the next page (`"0"` means done).

### Lists

```http
POST   /list/{key}           ["item1", "item2"]   # LPUSH (?side=right for RPUSH)
DELETE /list/{key}                                # LPOP  (?side=right for RPOP)
GET    /list/{key}?start=0&stop=-1                # LRANGE
```

### Sets

```http
POST   /set/{key}            ["member1", "member2"]   # SADD
GET    /set/{key}                                     # SMEMBERS
DELETE /set/{key}/{member}                            # SREM
```

### Hashes

```http
PUT    /hash/{key}/{field}   {"value": "data"}   # HSET
GET    /hash/{key}/{field}                       # HGET
DELETE /hash/{key}/{field}                       # HDEL
GET    /hash/{key}                               # HGETALL as an object
```

### Any command

```http
POST /command   {"args": ["HINCRBY", "user:1", "visits", "1"]}
POST /command   ["MGET", "a", "b"]
```

Runs any [supported Redis command](README.md#redis-clients). Replies are converted to JSON: integers → numbers, bulk/status strings → strings, nil → `null`, arrays → arrays.

### Lua scripts

```http
POST /eval
{
  "script": "return redis.call('INCRBY', KEYS[1], ARGV[1])",
  "keys": ["counter"],
  "args": ["5"]
}
```

### Pub/Sub

```http
POST /publish/{channel}     {"message": "Hello World"}   # count = subscribers reached
GET  /subscribe/{channel}   (WebSocket)
```

WebSocket subscribers receive each message as a text frame. Publishers on the REST API, RESP and the text protocol all reach the same subscribers. Slow subscribers that fall more than 1024 messages behind are disconnected so they can't hold up the server. Cross-origin WebSocket connections must come from an origin in `server.rest.cors_origins`.

### Server

```http
GET /ping       # {"value": "PONG"}
GET /health     # {"status": "ok", "version": "2.0.0"}
GET /ready
GET /info       # keys, memory, hits/misses, evictions, clients...
GET /metrics    # Prometheus text format
```

### Tokens (when auth is enabled)

```http
GET    /auth/tokens            # masked list with usage statistics
POST   /auth/tokens            # create a runtime token (valid for every protocol until restart)
DELETE /auth/tokens/{token}    # revoke a runtime token (value or masked id)
```

## Examples

### cURL

```bash
curl -X PUT localhost:8090/cache/user:1 -H "Content-Type: application/json" -d '{"value": "John Doe"}'
curl localhost:8090/cache/user:1
curl -X PUT localhost:8090/cache/session:abc -d '{"value": "active", "ttl": "5m"}'
curl -X POST localhost:8090/list/queue -d '["job1", "job2"]'
curl -X PUT localhost:8090/hash/user:2/name -d '{"value": "Ada"}'
curl -X POST localhost:8090/publish/news -d '{"message": "Hello World!"}'
```

More in [examples/rest-examples.sh](examples/rest-examples.sh).

### JavaScript

```javascript
const api = 'http://localhost:8090';
const headers = { 'Content-Type': 'application/json', Authorization: 'Bearer my-token' };

await fetch(`${api}/cache/user:1`, { method: 'PUT', headers, body: JSON.stringify({ value: 'John Doe', ttl: '1h' }) });
const { value } = await (await fetch(`${api}/cache/user:1`, { headers })).json();

const ws = new WebSocket(`ws://localhost:8090/subscribe/notifications?token=my-token`);
ws.onmessage = (event) => console.log('Message:', event.data);
```

## CORS

`server.rest.cors_enabled` and `server.rest.cors_origins` control CORS. By default only the server's own origin (the built-in web client) is allowed. List your front-end origins to call the API from other sites; `["*"]` allows any website, so only use it on trusted networks.

As CSRF protection, state-changing requests (anything but `GET`/`HEAD`) that carry an `Origin` header not in the list get `403`. Clients that don't send `Origin` (curl, servers, SDKs) are unaffected.
