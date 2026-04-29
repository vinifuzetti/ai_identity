package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"

	"github.com/vinifuzetti/ai_identity/internal/audit"
	"github.com/vinifuzetti/ai_identity/internal/mcptoken"
	ispiffe "github.com/vinifuzetti/ai_identity/internal/spiffe"
)

// contextKey é o tipo para chaves de context.Value — evita colisão com outros pacotes.
type contextKey string

const claimsKey contextKey = "claims"

func main() {
	audit.Init("mcp-server")

	jwksURL := env("JWKS_URL", "http://auth-server:8080/keys")
	expectedAud := env("EXPECTED_AUD", "https://mcp-server.internal/api")
	addr := env("ADDR", ":8081")
	mtlsAddr := env("MTLS_ADDR", ":8082")
	socketPath := env("SPIRE_AGENT_SOCKET", "")

	validator := mcptoken.NewValidator(jwksURL, expectedAud)

	mux := http.NewServeMux()
	mux.Handle("GET /tools", authMiddleware(validator, handleListTools))
	mux.Handle("POST /tools/call", authMiddleware(validator, handleCallTool))

	// Inicia listener mTLS se o socket do SPIRE Agent estiver disponível.
	if socketPath != "" {
		go startMTLSServer(socketPath, mtlsAddr, mux)
	}

	slog.Info("MCP Server HTTP iniciando", "addr", addr, "jwks_url", jwksURL)
	if err := http.ListenAndServe(addr, mux); err != nil {
		slog.Error("servidor HTTP encerrado", "error", err)
		os.Exit(1)
	}
}

// startMTLSServer inicia o listener mTLS SPIFFE na porta configurada.
// Apresenta o SVID X.509 do mcp-server e exige SVID válido do trust domain.
func startMTLSServer(socketPath, addr string, handler http.Handler) {
	ctx := context.Background()

	source, err := ispiffe.NewX509Source(ctx, socketPath,
		"spiffe://empresa.com/mcp/tools-server")
	if err != nil {
		slog.Error("falha ao criar X509Source para mTLS", "error", err)
		return
	}
	defer source.Close()

	tlsConfig, err := ispiffe.MTLSServerConfig(source, "empresa.com")
	if err != nil {
		slog.Error("falha ao criar mTLS config", "error", err)
		return
	}

	ln, err := tls.Listen("tcp", addr, tlsConfig)
	if err != nil {
		slog.Error("falha ao iniciar listener mTLS", "addr", addr, "error", err)
		return
	}

	svid, _ := source.GetX509SVID()
	slog.Info("MCP Server mTLS iniciando",
		"addr", addr,
		"spiffe_id", svid.ID.String(),
	)

	if err := http.Serve(ln, handler); err != nil {
		slog.Error("servidor mTLS encerrado", "error", err)
	}
}

// authMiddleware valida o composite token Bearer e injeta as claims no contexto.
func authMiddleware(v *mcptoken.Validator, next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)
		tokenStr := bearerToken(r)

		if tokenStr == "" {
			audit.MCPAccessDenied("token_ausente", r.Method, r.URL.Path, ip)
			jsonError(w, http.StatusUnauthorized, "token ausente")
			return
		}

		claims, err := v.Validate(tokenStr)
		if err != nil {
			audit.MCPAccessDenied("token_invalido", r.Method, r.URL.Path, ip)
			jsonError(w, http.StatusUnauthorized, "token inválido: "+err.Error())
			return
		}

		audit.MCPAccessAuthorized(claims.Subject, claims.AgentSPIFFEID(), claims.JTI,
			r.Method, r.URL.Path, ip)

		ctx := context.WithValue(r.Context(), claimsKey, claims)
		next(w, r.WithContext(ctx))
	})
}

// claimsFromContext extrai as claims do composite token do contexto da requisição.
func claimsFromContext(ctx context.Context) *mcptoken.CompositeClaims {
	c, _ := ctx.Value(claimsKey).(*mcptoken.CompositeClaims)
	return c
}

// handleListTools retorna a lista de ferramentas disponíveis no MCP Server.
func handleListTools(w http.ResponseWriter, r *http.Request) {
	c := claimsFromContext(r.Context())
	audit.MCPToolCalled(c.Subject, c.AgentSPIFFEID(), c.JTI, "list_tools", "success")

	tools := []map[string]string{
		{"name": "knowledge_search", "description": "Busca semântica na base de conhecimento corporativa"},
		{"name": "document_read", "description": "Leitura de documentos internos por ID"},
		{"name": "policy_check", "description": "Verifica se uma ação está em conformidade com a política IAM"},
	}
	jsonOK(w, map[string]any{"tools": tools})
}

// handleCallTool executa uma ferramenta MCP pelo nome informado no body.
func handleCallTool(w http.ResponseWriter, r *http.Request) {
	c := claimsFromContext(r.Context())

	var req struct {
		Tool   string         `json:"tool"`
		Params map[string]any `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		audit.MCPToolCalled(c.Subject, c.AgentSPIFFEID(), c.JTI, "unknown", "error")
		jsonError(w, http.StatusBadRequest, "body inválido: "+err.Error())
		return
	}

	switch req.Tool {
	case "knowledge_search":
		query, _ := req.Params["query"].(string)
		audit.MCPToolCalled(c.Subject, c.AgentSPIFFEID(), c.JTI, req.Tool, "success")
		jsonOK(w, map[string]any{
			"tool":    req.Tool,
			"results": []string{"Resultado simulado para: " + query},
		})
	case "document_read":
		id, _ := req.Params["id"].(string)
		audit.MCPToolCalled(c.Subject, c.AgentSPIFFEID(), c.JTI, req.Tool, "success")
		jsonOK(w, map[string]any{
			"tool":    req.Tool,
			"content": "Conteúdo simulado do documento ID=" + id,
		})
	case "policy_check":
		action, _ := req.Params["action"].(string)
		audit.MCPToolCalled(c.Subject, c.AgentSPIFFEID(), c.JTI, req.Tool, "success")
		jsonOK(w, map[string]any{
			"tool":    req.Tool,
			"action":  action,
			"allowed": true,
		})
	default:
		audit.MCPToolCalled(c.Subject, c.AgentSPIFFEID(), c.JTI, req.Tool, "not_found")
		jsonError(w, http.StatusNotFound, "ferramenta desconhecida: "+req.Tool)
	}
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if after, ok := strings.CutPrefix(h, "Bearer "); ok {
		return strings.TrimSpace(after)
	}
	return ""
}

// clientIP extrai o IP do cliente, preferindo o peer TLS quando disponível.
func clientIP(r *http.Request) string {
	if r.TLS != nil && len(r.TLS.PeerCertificates) > 0 {
		// Conexão mTLS: inclui URIs do certificado do peer
		for _, uri := range r.TLS.PeerCertificates[0].URIs {
			if strings.HasPrefix(uri.String(), "spiffe://") {
				return uri.String()
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func jsonOK(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func jsonError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
