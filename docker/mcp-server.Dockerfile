FROM golang:1.22-alpine AS builder
WORKDIR /src

# Copia módulo raiz (mcptoken + cmd/mcp-server)
COPY go.mod go.sum ./
RUN go mod download

COPY internal/ ./internal/
COPY cmd/mcp-server/ ./cmd/mcp-server/

RUN CGO_ENABLED=0 go build -o /mcp-server ./cmd/mcp-server

# Imagem final mínima
FROM alpine:3.19
COPY --from=builder /mcp-server /mcp-server
ENTRYPOINT ["/mcp-server"]
