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

// MCPAccessAuthorized registra um acesso autorizado ao MCP Server.
func MCPAccessAuthorized(sub, agent, jti, method, path, ip string) {
	slog.Info("mcp_access_authorized",
		"event", "mcp_access_authorized",
		"status", "success",
		"sub", sub,
		"agent", agent,
		"jti", jti,
		"method", method,
		"path", path,
		"ip", ip,
	)
}

// MCPAccessDenied registra um acesso negado ao MCP Server.
func MCPAccessDenied(reason, method, path, ip string) {
	slog.Warn("mcp_access_denied",
		"event", "mcp_access_denied",
		"status", "denied",
		"reason", reason,
		"method", method,
		"path", path,
		"ip", ip,
	)
}

// MCPToolCalled registra a invocação de uma ferramenta MCP.
func MCPToolCalled(sub, agent, jti, tool, status string) {
	slog.Info("mcp_tool_called",
		"event", "mcp_tool_called",
		"status", status,
		"sub", sub,
		"agent", agent,
		"jti", jti,
		"tool", tool,
	)
}
