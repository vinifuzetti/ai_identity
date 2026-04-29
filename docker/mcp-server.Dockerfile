FROM golang:1.22-alpine AS builder
WORKDIR /src

# Copia módulo raiz (mcptoken + cmd/mcp-server)
COPY go.mod go.sum ./
RUN go mod download

COPY internal/ ./internal/
COPY cmd/mcp-server/ ./cmd/mcp-server/

RUN CGO_ENABLED=0 go build -o /mcp-server ./cmd/mcp-server

# Imagem final mínima
# mcpserver (uid 1000) → seletor unix:uid:1000 no SPIRE,
# distinto do agent-workload (uid 0) → identidades X.509 separadas.
FROM alpine:3.19
RUN adduser -D -u 1000 mcpserver
COPY --from=builder /mcp-server /mcp-server
USER mcpserver
ENTRYPOINT ["/mcp-server"]
