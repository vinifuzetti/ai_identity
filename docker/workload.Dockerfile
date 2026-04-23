FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /agent-workload ./cmd/agent-workload

FROM alpine:3.19
COPY --from=builder /agent-workload /agent-workload
ENTRYPOINT ["/agent-workload"]
