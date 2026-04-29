#!/bin/sh
# Executa após `docker compose up` para registrar os workloads.
# Idempotente: recria a entry se o parentID (agent) tiver mudado.
# Remove agentes expirados antes de selecionar o agente ativo.

set -e

SOCKET="-socketPath /opt/spire/sockets/server.sock"
SPIFFE_ID="spiffe://empresa.com/agente/assistente-v2"
NOW=$(date -u +%s)

echo "==> Limpando agentes expirados do SPIRE Server..."
docker exec spire-server \
  /opt/spire/bin/spire-server agent list $SOCKET 2>/dev/null \
  | grep "SPIFFE ID" | awk -F': ' '{print $2}' \
  | while read -r AGENT; do
      EXPIRY=$(docker exec spire-server \
        /opt/spire/bin/spire-server agent show $SOCKET \
        -spiffeID "$AGENT" 2>/dev/null \
        | grep "Expiration time" | awk -F': ' '{print $2}' | xargs)

      # Converte "2026-04-24 03:44:36 +0000 UTC" → epoch (apenas data+hora)
      EXPIRY_EPOCH=$(date -u -j -f "%Y-%m-%d %H:%M:%S" \
        "$(echo "$EXPIRY" | awk '{print $1, $2}')" +%s 2>/dev/null || echo 9999999999)

      if [ "$EXPIRY_EPOCH" -lt "$NOW" ]; then
        echo "Evictando agente expirado: $AGENT"
        docker exec spire-server \
          /opt/spire/bin/spire-server agent evict $SOCKET \
          -spiffeID "$AGENT" 2>/dev/null || true
      fi
    done

AGENT_ID=$(docker exec spire-server \
  /opt/spire/bin/spire-server agent list $SOCKET 2>/dev/null \
  | grep "SPIFFE ID" | awk -F': ' '{print $2}' | head -1)

if [ -z "$AGENT_ID" ]; then
  echo "ERRO: nenhum agent registrado. Execute 'make up' primeiro."
  exit 1
fi

echo "Agent SPIFFE ID: $AGENT_ID"

# Verifica se já existe uma entry para este SPIFFE ID
EXISTING=$(docker exec spire-server \
  /opt/spire/bin/spire-server entry show $SOCKET \
  -spiffeID "$SPIFFE_ID" 2>/dev/null) || true

if echo "$EXISTING" | grep -q "Entry ID"; then
  EXISTING_PARENT=$(echo "$EXISTING" | grep "Parent ID" | awk -F': ' '{print $2}' | tr -d ' ')

  if [ "$EXISTING_PARENT" = "$AGENT_ID" ]; then
    echo "Entry já existente com parentID correto — nenhuma ação necessária."
    exit 0
  fi

  ENTRY_ID=$(echo "$EXISTING" | grep "Entry ID" | awk -F': ' '{print $2}' | tr -d ' ')
  echo "parentID desatualizado. Removendo entry antiga (ID: $ENTRY_ID)..."
  docker exec spire-server \
    /opt/spire/bin/spire-server entry delete $SOCKET \
    -entryID "$ENTRY_ID"
fi

echo "Criando entry para $SPIFFE_ID..."
docker exec spire-server \
  /opt/spire/bin/spire-server entry create $SOCKET \
  -spiffeID "$SPIFFE_ID" \
  -parentID  "$AGENT_ID" \
  -selector  unix:uid:0

echo "Entry criada com sucesso."
