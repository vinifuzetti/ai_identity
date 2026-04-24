#!/bin/sh
set -e

AUTH_SERVER="${AUTH_SERVER:-http://localhost:8080}"

echo "==> Gerando JWT do cliente (mock IdP)..."
CLIENT_JWT=$(docker exec auth-server /gen-client-jwt)
echo "Client JWT: ${CLIENT_JWT:0:60}..."

echo ""
echo "==> Buscando SVID JWT do agente via SPIRE..."
SVID=$(docker exec spire-agent \
  /opt/spire/bin/spire-agent api fetch jwt \
    -audience empresa.com \
    -socketPath /opt/spire/sockets/agent.sock \
  | grep -A1 "^token(" | tail -1 | tr -d '\t ')
echo "SVID: ${SVID:0:60}..."

echo ""
echo "==> Executando Token Exchange (RFC 8693)..."
RESPONSE=$(curl -s -X POST "$AUTH_SERVER/token" \
  -H "Content-Type: application/x-www-form-urlencoded" \
  --data-urlencode "grant_type=urn:ietf:params:oauth:grant-type:token-exchange" \
  --data-urlencode "subject_token=$CLIENT_JWT" \
  --data-urlencode "subject_token_type=urn:ietf:params:oauth:token-type:jwt" \
  --data-urlencode "actor_token=$SVID" \
  --data-urlencode "actor_token_type=urn:ietf:params:oauth:token-type:jwt" \
  --data-urlencode "client_assertion_type=urn:ietf:params:oauth:client-assertion-type:jwt-bearer" \
  --data-urlencode "client_assertion=$SVID" \
  --data-urlencode "resource=https://mcp-server.internal/api" \
  --data-urlencode "scope=mcp:tools:read mcp:knowledge:search")

echo ""
echo "==> Resposta do Auth Server:"
echo "$RESPONSE" | python3 -m json.tool 2>/dev/null || echo "$RESPONSE"
