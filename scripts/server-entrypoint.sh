#!/bin/sh
set -e

SOCKET="/opt/spire/sockets/server.sock"
BUNDLE_DIR="/opt/spire/bundle"

/opt/spire/bin/spire-server run -config /opt/spire/conf/server/server.conf &
SERVER_PID=$!

# Aguarda o server ficar pronto
until /opt/spire/bin/spire-server healthcheck -socketPath "$SOCKET" 2>/dev/null; do
  sleep 1
done

# Exporta o bundle JWT como JWKS e renova a cada 60s
# O auth-server lê deste arquivo — sem necessidade de SPIRE Agent co-localizado
mkdir -p "$BUNDLE_DIR"
while true; do
  /opt/spire/bin/spire-server bundle show \
    -format spiffe \
    -socketPath "$SOCKET" \
    > "$BUNDLE_DIR/jwks.json.tmp" 2>/dev/null && \
  mv "$BUNDLE_DIR/jwks.json.tmp" "$BUNDLE_DIR/jwks.json"
  sleep 60
done &

wait $SERVER_PID
