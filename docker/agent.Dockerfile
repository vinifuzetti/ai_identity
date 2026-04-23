FROM ghcr.io/spiffe/spire-agent:1.9.6 AS spire
FROM alpine:3.19
COPY --from=spire /opt/spire/bin/spire-agent /opt/spire/bin/spire-agent
