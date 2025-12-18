# BoltCache Authentication Guide 🔐

Token-based authentication for secure API access.

## Quick Start

```bash
# Run with auth enabled (dev config)
make run-dev

# Test authentication
make test-auth

# Open web client with auth
make web-client
```

## Authentication Methods

### 1. Authorization Header (Recommended)
```bash
curl -H "Authorization: Bearer your-token-here" \
  http://localhost:8080/cache/key
```

### 2. X-API-Token Header
```bash
curl -H "X-API-Token: your-token-here" \
  http://localhost:8080/cache/key
```

### 3. Query Parameter
```bash
curl "http://localhost:8080/cache/key?token=your-token-here"
```

## Configuration

### Enable Authentication
```yaml
# config.yaml
security:
  auth:
    enabled: true
    method: "token"
    tokens:
      - "your-secret-token-1"
      - "your-secret-token-2"
```

### Development Config
```yaml
# config-dev.yaml
security:
  auth:
    enabled: true
    tokens:
      - "dev-token-123"
      - "test-token-456"
```

### Production Config
```yaml
# config-prod.yaml
security:
  auth:
    enabled: true
    tokens:
      - "prod-secure-token-xyz"
```

## Token Management API

### List Tokens
```bash
GET /auth/tokens
Authorization: Bearer your-token

Response:
{
  "success": true,
  "value": {
    "token1": {
      "token": "token1",
      "created_at": "2024-01-01T00:00:00Z",
      "last_used": "2024-01-01T12:00:00Z",
      "usage_count": 42
    }
  }
}
```

### Create Token
```bash
POST /auth/tokens
Authorization: Bearer your-token

Response:
{
  "success": true,
  "value": {
    "token": "newly-generated-token-abc123"
  }
}
```

### Delete Token
```bash
DELETE /auth/tokens/{token}
Authorization: Bearer your-token

Response:
{
  "success": true
}
```

## Protected Endpoints

All API endpoints require authentication except:
- `GET /ping` - Health check
- `GET /health` - Health status

### Protected Endpoints:
- All cache operations (`/cache/*`)
- All data type operations (`/list/*`, `/set/*`, `/hash/*`)
- Pub/Sub operations (`/publish/*`, `/subscribe/*`)
- Script execution (`/eval`)
- Server info (`/info`)
- Token management (`/auth/*`)

## Error Responses

### 401 Unauthorized
```json
{
  "success": false,
  "error": "Authentication required"
}
```

Returned when:
- No token provided
- Invalid token
- Token not found

## Security Best Practices

### Token Generation
- Use cryptographically secure random tokens
- Minimum 32 characters length
- Include letters, numbers, and symbols

### Token Storage
- Store tokens securely (environment variables, secrets manager)
- Never commit tokens to version control
- Rotate tokens regularly

### Production Deployment
```yaml
# Use environment variables
security:
  auth:
    enabled: true
    tokens:
      - "${BOLTCACHE_TOKEN_1}"
      - "${BOLTCACHE_TOKEN_2}"
```

```bash
# Set environment variables
export BOLTCACHE_TOKEN_1="your-production-token-1"
export BOLTCACHE_TOKEN_2="your-production-token-2"
```

## JavaScript Client Example

```javascript
class BoltCacheClient {
  constructor(baseUrl, token) {
    this.baseUrl = baseUrl;
    this.token = token;
  }

  async request(method, path, body = null) {
    const options = {
      method,
      headers: {
        'Content-Type': 'application/json',
        'Authorization': `Bearer ${this.token}`
      }
    };
    
    if (body) {
      options.body = JSON.stringify(body);
    }

    const response = await fetch(`${this.baseUrl}${path}`, options);
    return response.json();
  }

  async set(key, value, ttl = null) {
    const body = { value };
    if (ttl) body.ttl = ttl;
    return this.request('PUT', `/cache/${key}`, body);
  }

  async get(key) {
    return this.request('GET', `/cache/${key}`);
  }
}

// Usage
const client = new BoltCacheClient('http://localhost:8080', 'your-token');
await client.set('user:1', 'John Doe');
const result = await client.get('user:1');
```

## Rate Limiting

Combine with rate limiting for additional security:

```yaml
security:
  auth:
    enabled: true
    tokens: ["your-token"]
  rate_limit:
    enabled: true
    requests_per_second: 1000
    burst: 100
```

## Monitoring

Track authentication metrics:
- Failed authentication attempts
- Token usage statistics
- Most active tokens
- Authentication errors

Bu authentication sistemi ile BoltCache API'niz tamamen güvenli! 🛡️