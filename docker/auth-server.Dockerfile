FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY auth-server/go.mod auth-server/go.sum ./
RUN go mod download
COPY auth-server/ .
RUN go build -o /auth-server ./cmd/server
RUN go build -o /gen-client-jwt ./cmd/gen-client-jwt

FROM alpine:3.19
COPY --from=builder /auth-server /auth-server
COPY --from=builder /gen-client-jwt /gen-client-jwt
EXPOSE 8080
ENTRYPOINT ["/auth-server"]
