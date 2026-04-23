FROM ghcr.io/spiffe/spire-server:1.9.6 AS spire
FROM alpine:3.19
COPY --from=spire /opt/spire/bin/spire-server /opt/spire/bin/spire-server
