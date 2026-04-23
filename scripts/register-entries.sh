#!/bin/sh
# Executa após `docker compose up` para registrar os workloads

SOCKET="-socketPath /opt/spire/sockets/server.sock"

AGENT_ID=$(docker exec spire-server \
  /opt/spire/bin/spire-server agent list $SOCKET \
  | grep "SPIFFE ID" | awk -F': ' '{print $2}' | head -1)

echo "Agent SPIFFE ID: $AGENT_ID"

docker exec spire-server \
  /opt/spire/bin/spire-server entry create $SOCKET \
  -spiffeID spiffe://empresa.com/agente/assistente-v2 \
  -parentID  "$AGENT_ID" \
  -selector  unix:uid:0

echo "Entry criada com sucesso."
