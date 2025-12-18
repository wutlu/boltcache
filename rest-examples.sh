#!/bin/bash

# BoltCache REST API Examples

BASE_URL="http://localhost:8090"

echo "🚀 BoltCache REST API Examples"
echo "================================"

# Health check
echo -e "\n1. Health Check (PING)"
curl -X GET "$BASE_URL/ping"

# Set value
echo -e "\n\n2. Set Value"
curl -X PUT "$BASE_URL/cache/user:1" \
  -H "Content-Type: application/json" \
  -d '{"value": "John Doe"}'

# Get value
echo -e "\n\n3. Get Value"
curl -X GET "$BASE_URL/cache/user:1"

# Set with TTL
echo -e "\n\n4. Set with TTL (5 minutes)"
curl -X PUT "$BASE_URL/cache/session:abc" \
  -H "Content-Type: application/json" \
  -d '{"value": "active", "ttl": "5m"}'

# List operations
echo -e "\n\n5. List Push"
curl -X POST "$BASE_URL/list/mylist" \
  -H "Content-Type: application/json" \
  -d '["item1", "item2", "item3"]'

echo -e "\n\n6. List Pop"
curl -X DELETE "$BASE_URL/list/mylist"

# Set operations
echo -e "\n\n7. Set Add"
curl -X POST "$BASE_URL/set/myset" \
  -H "Content-Type: application/json" \
  -d '["member1", "member2", "member3"]'

echo -e "\n\n8. Set Members"
curl -X GET "$BASE_URL/set/myset"

# Hash operations
echo -e "\n\n9. Hash Set"
curl -X PUT "$BASE_URL/hash/user:1/name" \
  -H "Content-Type: application/json" \
  -d '{"value": "John"}'

curl -X PUT "$BASE_URL/hash/user:1/age" \
  -H "Content-Type: application/json" \
  -d '{"value": "30"}'

echo -e "\n\n10. Hash Get"
curl -X GET "$BASE_URL/hash/user:1/name"

# Publish message
echo -e "\n\n11. Publish Message"
curl -X POST "$BASE_URL/publish/notifications" \
  -H "Content-Type: application/json" \
  -d '{"message": "Hello World!"}'

# Execute script
echo -e "\n\n12. Execute Lua Script"
curl -X POST "$BASE_URL/eval" \
  -H "Content-Type: application/json" \
  -d '{
    "script": "redis.call(\"SET\", KEYS[1], ARGV[1])",
    "keys": ["scriptkey"],
    "args": ["scriptvalue"]
  }'

# Server info
echo -e "\n\n13. Server Info"
curl -X GET "$BASE_URL/info"

# Delete key
echo -e "\n\n14. Delete Key"
curl -X DELETE "$BASE_URL/cache/user:1"

echo -e "\n\n✅ All examples completed!"