#!/bin/bash

# BoltCache Pub/Sub Test Script

BASE_URL="${BASE_URL:-http://localhost:8090}"
TOKEN="${TOKEN:-dev-token-123}"  # matches config-dev.yaml; ignored when auth is disabled

echo "🚀 BoltCache Pub/Sub Test"
echo "========================="

# Test 1: Publish to empty channel
echo -e "\n1. Publishing to empty channel..."
curl -H "Authorization: Bearer $TOKEN" -s -X POST "$BASE_URL/publish/test-channel" \
  -H "Content-Type: application/json" \
  -d '{"message": "Hello World!"}' | jq .

# Test 2: Multiple publishes
echo -e "\n2. Multiple messages..."
for i in {1..3}; do
  echo "Publishing message $i..."
  curl -H "Authorization: Bearer $TOKEN" -s -X POST "$BASE_URL/publish/notifications" \
    -H "Content-Type: application/json" \
    -d "{\"message\": \"Message $i from script\"}" | jq .
  sleep 1
done

# Test 3: Different channels
echo -e "\n3. Different channels..."
curl -H "Authorization: Bearer $TOKEN" -s -X POST "$BASE_URL/publish/user-events" \
  -H "Content-Type: application/json" \
  -d '{"message": "User logged in"}' | jq .

curl -H "Authorization: Bearer $TOKEN" -s -X POST "$BASE_URL/publish/system-alerts" \
  -H "Content-Type: application/json" \
  -d '{"message": "System maintenance in 5 minutes"}' | jq .

echo -e "\n✅ Pub/Sub test completed!"
echo -e "\n📝 To test WebSocket subscription:"
echo "   1. Open: make web-client"
echo "   2. Subscribe to 'notifications' channel"
echo "   3. Run this script again to see real-time messages"