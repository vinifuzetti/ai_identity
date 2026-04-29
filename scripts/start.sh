#!/bin/sh
set -e

COMPOSE="${COMPOSE:-docker-compose}"
SOCKET="/opt/spire/sockets/server.sock"

echo "==> Subindo SPIRE Server..."
$COMPOSE up -d spire-server

echo "==> Aguardando SPIRE Server ficar pronto..."
until docker exec spire-server \
    /opt/spire/bin/spire-server healthcheck -socketPath "$SOCKET" 2>/dev/null; do
  sleep 2
done
echo "==> SPIRE Server pronto."

echo "==> Gerando join token..."
TOKEN=$(docker exec spire-server \
  /opt/spire/bin/spire-server token generate -ttl 600 -socketPath "$SOCKET" \
  | grep "Token:" | awk '{print $2}')

if [ -z "$TOKEN" ]; then
  echo "ERRO: falha ao extrair token. Output bruto:"
  docker exec spire-server \
    /opt/spire/bin/spire-server token generate -ttl 60 -socketPath "$SOCKET"
  exit 1
fi

echo "==> Token gerado: $TOKEN"

echo "==> Recriando SPIRE Agent (remove dados antigos para evitar bundle stale)..."
$COMPOSE rm -fsv spire-agent 2>/dev/null || true
docker volume rm ai_identity_spire-agent-data 2>/dev/null || true
JOIN_TOKEN=$TOKEN $COMPOSE up -d spire-agent

echo "==> Aguardando bundle JWT do SPIRE Server..."
until docker exec spire-server test -f /opt/spire/bundle/jwks.json 2>/dev/null; do
  sleep 1
done
echo "==> Bundle disponível."

echo "==> Subindo Authorization Server..."
$COMPOSE up -d auth-server

echo "==> Aguardando Authorization Server ficar pronto..."
until curl -sf http://localhost:8080/keys > /dev/null 2>&1; do
  sleep 2
done
echo "==> Auth Server pronto."

echo "==> Subindo MCP Server..."
$COMPOSE up -d mcp-server

echo "==> Ambiente pronto. Use 'make logs' para acompanhar."
