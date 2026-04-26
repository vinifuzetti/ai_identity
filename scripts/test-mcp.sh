#!/bin/sh
set -e

AUTH_SERVER="${AUTH_SERVER:-http://localhost:8080}"
MCP_SERVER="${MCP_SERVER:-http://localhost:8081}"

echo "==> Gerando JWT do cliente (mock IdP)..."
CLIENT_JWT=$(docker exec auth-server /gen-client-jwt)
echo "Client JWT: ${CLIENT_JWT:0:60}..."

echo ""
echo "==> Buscando SVID JWT do agente via SPIRE..."
SVID=$(docker exec spire-agent \
  /opt/spire/bin/spire-agent api fetch jwt \
    -audience empresa.com \
    -socketPath /opt/spire/sockets/agent.sock \
  2>/dev/null | grep -A1 "^token(" | tail -1 | tr -d '\t ')

if [ -z "$SVID" ]; then
  echo "ERRO: SVID vazio. Execute 'make register' antes de testar."
  exit 1
fi
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

COMPOSITE_TOKEN=$(echo "$RESPONSE" | python3 -c "import sys,json; print(json.load(sys.stdin)['access_token'])" 2>/dev/null)

if [ -z "$COMPOSITE_TOKEN" ]; then
  echo "ERRO: falha ao obter composite token. Resposta:"
  echo "$RESPONSE"
  exit 1
fi

echo "Composite Token: ${COMPOSITE_TOKEN:0:80}..."

echo ""
echo "==> [1/3] Listando ferramentas do MCP Server..."
curl -sf "$MCP_SERVER/tools" \
  -H "Authorization: Bearer $COMPOSITE_TOKEN" \
  | python3 -m json.tool

echo ""
echo "==> [2/3] Chamando ferramenta: knowledge_search..."
curl -sf -X POST "$MCP_SERVER/tools/call" \
  -H "Authorization: Bearer $COMPOSITE_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"tool":"knowledge_search","params":{"query":"política de acesso IAM"}}' \
  | python3 -m json.tool

echo ""
echo "==> [3/3] Chamando ferramenta: policy_check..."
curl -sf -X POST "$MCP_SERVER/tools/call" \
  -H "Authorization: Bearer $COMPOSITE_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"tool":"policy_check","params":{"action":"document:read"}}' \
  | python3 -m json.tool

echo ""
echo "==> Teste E2E concluído com sucesso."
