// Package audit fornece logging estruturado de eventos de segurança via log/slog.
// Todos os eventos são emitidos como JSON no stdout para facilitar ingestão por
// sistemas de SIEM (Splunk, Datadog, OpenSearch, etc.).
package audit

import (
	"log/slog"
	"os"
)

// Init inicializa o logger global com saída JSON no stdout.
// Deve ser chamado em main() antes de qualquer log.
func Init(service string) {
	h := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})
	slog.SetDefault(slog.New(h).With("service", service))
}

// TokenExchangeSuccess registra um token exchange bem-sucedido.
func TokenExchangeSuccess(sub, agent, jti, resource, scope, ip string) {
	slog.Info("token_exchange_success",
		"event", "token_exchange_success",
		"status", "success",
		"sub", sub,
		"agent", agent,
		"jti", jti,
		"resource", resource,
		"scope", scope,
		"ip", ip,
	)
}

// TokenExchangeDenied registra um token exchange rejeitado.
func TokenExchangeDenied(reason, ip string, err error) {
	attrs := []any{
		"event", "token_exchange_denied",
		"status", "denied",
		"reason", reason,
		"ip", ip,
	}
	if err != nil {
		attrs = append(attrs, "error", err.Error())
	}
	slog.Warn("token_exchange_denied", attrs...)
}

// TokenExchangeError registra um erro interno durante o token exchange.
func TokenExchangeError(reason, ip string, err error) {
	slog.Error("token_exchange_error",
		"event", "token_exchange_error",
		"status", "error",
		"reason", reason,
		"ip", ip,
		"error", err.Error(),
	)
}
