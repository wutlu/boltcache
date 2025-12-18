#!/bin/bash

# BoltCache Authentication Examples

BASE_URL="http://localhost:8090"
TOKEN="dev-token-123"

echo "🔐 BoltCache Authentication Examples"
echo "===================================="

# Test without token (should fail)
echo -e "\n1. Test without token (should fail)"
curl -X GET "$BASE_URL/cache/test" -w "\nStatus: %{http_code}\n"

# Test with invalid token (should fail)
echo -e "\n2. Test with invalid token (should fail)"
curl -X GET "$BASE_URL/cache/test" \
  -H "Authorization: Bearer invalid-token" \
  -w "\nStatus: %{http_code}\n"

# Test with valid token (should work)
echo -e "\n3. Test with valid token (should work)"
curl -X PUT "$BASE_URL/cache/test" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"value": "authenticated"}' \
  -w "\nStatus: %{http_code}\n"

# Get value with token
echo -e "\n4. Get value with token"
curl -X GET "$BASE_URL/cache/test" \
  -H "Authorization: Bearer $TOKEN" \
  -w "\nStatus: %{http_code}\n"

# Test different auth methods
echo -e "\n5. Test X-API-Token header"
curl -X GET "$BASE_URL/cache/test" \
  -H "X-API-Token: $TOKEN" \
  -w "\nStatus: %{http_code}\n"

echo -e "\n6. Test query parameter"
curl -X GET "$BASE_URL/cache/test?token=$TOKEN" \
  -w "\nStatus: %{http_code}\n"

# List tokens (requires auth)
echo -e "\n7. List tokens"
curl -X GET "$BASE_URL/auth/tokens" \
  -H "Authorization: Bearer $TOKEN"

# Create new token
echo -e "\n\n8. Create new token"
NEW_TOKEN=$(curl -s -X POST "$BASE_URL/auth/tokens" \
  -H "Authorization: Bearer $TOKEN" | \
  grep -o '"token":"[^"]*"' | \
  cut -d'"' -f4)

echo "New token: $NEW_TOKEN"

# Test with new token
echo -e "\n9. Test with new token"
curl -X GET "$BASE_URL/cache/test" \
  -H "Authorization: Bearer $NEW_TOKEN" \
  -w "\nStatus: %{http_code}\n"

# Delete token
echo -e "\n10. Delete token"
curl -X DELETE "$BASE_URL/auth/tokens/$NEW_TOKEN" \
  -H "Authorization: Bearer $TOKEN" \
  -w "\nStatus: %{http_code}\n"

# Test deleted token (should fail)
echo -e "\n11. Test deleted token (should fail)"
curl -X GET "$BASE_URL/cache/test" \
  -H "Authorization: Bearer $NEW_TOKEN" \
  -w "\nStatus: %{http_code}\n"

# Health check (no auth required)
echo -e "\n12. Health check (no auth required)"
curl -X GET "$BASE_URL/ping" \
  -w "\nStatus: %{http_code}\n"

echo -e "\n✅ Authentication examples completed!"