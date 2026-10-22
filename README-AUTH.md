# BoltCache Authentication 🔐

BoltCache uses API tokens. When `security.auth.enabled` is true, the same tokens protect **every** protocol: the REST API, the RESP listener and the TCP text protocol.

## Enabling authentication

```yaml
security:
  auth:
    enabled: true
    tokens:
      - "${BOLTCACHE_TOKEN}"        # taken from the environment at startup
      - "another-token"
```

```bash
export BOLTCACHE_TOKEN=$(openssl rand -hex 24)
boltcache server --config config-prod.yaml
```

`boltcache config validate` fails if auth is enabled without a non-empty token. With Docker: `docker run -e BOLTCACHE_AUTH=true -e BOLTCACHE_TOKEN=... ghcr.io/wutlu/boltcache`.

The dev configuration (`make run-dev`) enables auth with the tokens `dev-token-123` and `test-token-456`.

## Using a token

### REST API

```bash
# Authorization header (recommended)
curl -H "Authorization: Bearer $TOKEN" localhost:8090/cache/key

# X-API-Token header
curl -H "X-API-Token: $TOKEN" localhost:8090/cache/key

# Query parameter (needed for browser WebSockets, avoid elsewhere: URLs end up in logs)
curl "localhost:8090/cache/key?token=$TOKEN"
```

Requests without a valid token get `401 Unauthorized`. `/ping`, `/health`, `/ready`, `/`, `/docs` and `/openapi.json` are public so health checks and the web client work; `/metrics` requires a token (Prometheus supports `authorization: { credentials: ... }`).

### Redis clients (RESP) and the text protocol

```bash
redis-cli -p 6382 -a "$TOKEN"
```

```go
rdb := redis.NewClient(&redis.Options{Addr: "localhost:6382", Password: token})
```

```
$ nc localhost 6380
GET key
ERROR: NOAUTH Authentication required.
AUTH my-token
OK
```

`AUTH <token>`, `AUTH default <token>` and `HELLO 2 AUTH default <token>` are accepted. Until a client authenticates, only `AUTH`, `HELLO`, `PING`, `QUIT` and `RESET` are allowed.

## Managing tokens at runtime

```bash
# List tokens (masked) with usage statistics
curl -H "Authorization: Bearer $TOKEN" localhost:8090/auth/tokens

# Create a token
curl -X POST -H "Authorization: Bearer $TOKEN" localhost:8090/auth/tokens
# {"success":true,"value":{"token":"bc_6c0f...","note":"runtime token: kept in memory until the server restarts"}}

# Revoke it (by value or by the masked id shown in the list)
curl -X DELETE -H "Authorization: Bearer $TOKEN" localhost:8090/auth/tokens/bc_6c0f...
```

Runtime tokens work for every protocol and disappear on restart. Tokens from the config file can't be revoked at runtime; remove them from the file and restart.

## Recommendations

- Generate long random tokens (`openssl rand -hex 24`) and inject them through environment variables or secrets, not committed files.
- Keep `cors_origins` to your own front-end origins.
- Enable `security.rate_limit` on internet-facing REST endpoints.
- Use `security.tls` (or a TLS-terminating proxy) for the REST API. The RESP and TCP listeners are plain TCP: keep them on private networks.
- Token comparisons are constant-time, and `boltcache config show` masks tokens.

Try it end to end with [examples/auth-examples.sh](examples/auth-examples.sh) against `make run-dev`.
