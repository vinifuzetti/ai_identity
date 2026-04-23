#!/bin/sh
set -e

if [ -z "$JOIN_TOKEN" ]; then
  echo "[agent] ERRO: JOIN_TOKEN nao definido"
  exit 1
fi

echo "[agent] Iniciando com token: $JOIN_TOKEN"
exec /opt/spire/bin/spire-agent run \
  -config /opt/spire/conf/agent/agent.conf \
  -joinToken "$JOIN_TOKEN"
