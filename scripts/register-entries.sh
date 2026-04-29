#!/bin/sh
# Executa após `docker compose up` para registrar os workloads.
# Idempotente: recria a entry se o parentID (agent) tiver mudado.

set -e

SOCKET="-socketPath /opt/spire/sockets/server.sock"
SPIFFE_ID="spiffe://empresa.com/agente/assistente-v2"

AGENT_ID=$(docker exec spire-server \
  /opt/spire/bin/spire-server agent list $SOCKET \
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
  # Entry existe — verifica se o parentID bate com o agent atual
  EXISTING_PARENT=$(echo "$EXISTING" | grep "Parent ID" | awk -F': ' '{print $2}' | tr -d ' ')

  if [ "$EXISTING_PARENT" = "$AGENT_ID" ]; then
    echo "Entry já existente com parentID correto — nenhuma ação necessária."
    exit 0
  fi

  # parentID desatualizado (novo join token) — remove e recria
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
