#!/bin/sh
# Executa após `docker compose up` para registrar os workloads.
# Idempotente: recria entries se o parentID (agent) tiver mudado.
# Remove agentes expirados antes de selecionar o agente ativo.
#
# Workloads registrados:
#   spiffe://empresa.com/agente/assistente-v2  → selector unix:uid:0  (agent-workload, root)
#   spiffe://empresa.com/mcp/tools-server      → selector unix:uid:1000 (mcp-server, mcpserver user)

set -e

SOCKET="-socketPath /opt/spire/sockets/server.sock"
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

# Registra (ou recria) uma entry. Args: $1=spiffeID $2=selector
register_entry() {
  ENTRY_SPIFFE_ID="$1"
  SELECTOR="$2"

  EXISTING=$(docker exec spire-server \
    /opt/spire/bin/spire-server entry show $SOCKET \
    -spiffeID "$ENTRY_SPIFFE_ID" 2>/dev/null) || true

  if echo "$EXISTING" | grep -q "Entry ID"; then
    EXISTING_PARENT=$(echo "$EXISTING" | grep "Parent ID" | awk -F': ' '{print $2}' | tr -d ' ')
    if [ "$EXISTING_PARENT" = "$AGENT_ID" ]; then
      echo "[$ENTRY_SPIFFE_ID] entry correta — nenhuma ação necessária."
      return
    fi
    ENTRY_ID=$(echo "$EXISTING" | grep "Entry ID" | awk -F': ' '{print $2}' | tr -d ' ')
    echo "[$ENTRY_SPIFFE_ID] parentID desatualizado — removendo entry antiga (ID: $ENTRY_ID)..."
    docker exec spire-server \
      /opt/spire/bin/spire-server entry delete $SOCKET \
      -entryID "$ENTRY_ID"
  fi

  echo "[$ENTRY_SPIFFE_ID] criando entry..."
  docker exec spire-server \
    /opt/spire/bin/spire-server entry create $SOCKET \
    -spiffeID "$ENTRY_SPIFFE_ID" \
    -parentID  "$AGENT_ID" \
    -selector  "$SELECTOR"
  echo "[$ENTRY_SPIFFE_ID] entry criada."
}

register_entry "spiffe://empresa.com/agente/assistente-v2"  "unix:uid:0"
register_entry "spiffe://empresa.com/mcp/tools-server"      "unix:uid:1000"

echo "==> Registro concluído."
