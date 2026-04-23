#!/bin/sh
set -e
exec /opt/spire/bin/spire-server run -config /opt/spire/conf/server/server.conf
