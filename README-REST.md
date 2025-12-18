# BoltCache REST API 🚀

Modern RESTful cache sistemi - HTTP/JSON ile Redis benzeri operasyonlar.

## Özellikler

- 🌐 **RESTful API**: HTTP/JSON protokolü
- 📡 **WebSocket Pub/Sub**: Real-time messaging
- 🎯 **CORS Support**: Web browser compatibility
- 📱 **Web Client**: Browser-based test interface
- 🔧 **cURL Examples**: Command-line testing
- ⚡ **Same Performance**: TCP versiyonu kadar hızlı

## Hızlı Başlangıç

```bash
# Dependencies yükle
go mod download

# REST server başlat
make run-rest

# Web client aç
make web-client

# cURL testleri çalıştır
make test-rest
```

## API Endpoints

### String Operations
```http
PUT    /cache/{key}           # Set value
GET    /cache/{key}           # Get value  
DELETE /cache/{key}           # Delete key
```

### List Operations
```http
POST   /list/{key}            # Push values
DELETE /list/{key}            # Pop value
```

### Set Operations
```http
POST   /set/{key}             # Add members
GET    /set/{key}             # Get members
```

### Hash Operations
```http
PUT    /hash/{key}/{field}    # Set field
GET    /hash/{key}/{field}    # Get field
```

### Pub/Sub
```http
GET    /subscribe/{channel}   # Subscribe (WebSocket)
POST   /publish/{channel}     # Publish message
```

### Scripting & Info
```http
POST   /eval                  # Execute Lua script
GET    /info                  # Server info
GET    /ping                  # Health check
```

## Örnek Kullanım

### cURL ile:
```bash
# Set value
curl -X PUT http://localhost:8090/cache/user:1 \
  -H "Content-Type: application/json" \
  -d '{"value": "John Doe"}'

# Get value
curl -X GET http://localhost:8090/cache/user:1

# Set with TTL
curl -X PUT http://localhost:8090/cache/session:abc \
  -H "Content-Type: application/json" \
  -d '{"value": "active", "ttl": "5m"}'

# List operations
curl -X POST http://localhost:8090/list/mylist \
  -H "Content-Type: application/json" \
  -d '["item1", "item2"]'

# Hash operations
curl -X PUT http://localhost:8090/hash/user:1/name \
  -H "Content-Type: application/json" \
  -d '{"value": "John"}'

# Publish message
curl -X POST http://localhost:8090/publish/news \
  -H "Content-Type: application/json" \
  -d '{"message": "Hello World!"}'
```

### JavaScript ile:
```javascript
// Set value
await fetch('http://localhost:8090/cache/user:1', {
  method: 'PUT',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ value: 'John Doe' })
});

// Get value
const response = await fetch('http://localhost:8090/cache/user:1');
const data = await response.json();
console.log(data.value); // "John Doe"

// WebSocket subscription
const ws = new WebSocket('ws://localhost:8090/subscribe/notifications');
ws.onmessage = (event) => console.log('Message:', event.data);
```

## Response Format

Tüm API yanıtları JSON formatında:

```json
{
  "success": true,
  "value": "response data",
  "count": 5,
  "error": "error message if any"
}
```

## Avantajları

**TCP Versiyonuna Göre:**
- ✅ Web browser compatibility
- ✅ Standard HTTP status codes
- ✅ JSON format (parse kolay)
- ✅ CORS support
- ✅ WebSocket pub/sub
- ✅ Existing HTTP tools kullanılabilir

**Redis'e Göre:**
- ✅ RESTful design
- ✅ Modern web standards
- ✅ Microservice friendly
- ✅ Cloud-native
- ✅ API Gateway compatible

## Deployment

```bash
# Docker build
docker build -t boltcache-rest .

# Docker run
docker run -p 8080:8080 boltcache-rest -rest

# Kubernetes deployment
kubectl apply -f k8s-deployment.yaml
```

Bu RESTful yaklaşım modern web uygulamaları için çok daha uygun! 🎯