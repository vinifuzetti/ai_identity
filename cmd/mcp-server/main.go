package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/vinifuzetti/ai_identity/internal/mcptoken"
)

func main() {
	jwksURL := env("JWKS_URL", "http://auth-server:8080/keys")
	expectedAud := env("EXPECTED_AUD", "https://mcp-server.internal/api")
	addr := env("ADDR", ":8081")

	validator := mcptoken.NewValidator(jwksURL, expectedAud)

	mux := http.NewServeMux()
	mux.Handle("GET /tools", auth(validator, handleListTools))
	mux.Handle("POST /tools/call", auth(validator, handleCallTool))

	log.Printf("MCP Server escutando em %s", addr)
	log.Printf("JWKS URL: %s | Audience: %s", jwksURL, expectedAud)

	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("servidor encerrado: %v", err)
	}
}

// auth é o middleware de autenticação: valida o composite token Bearer.
func auth(v *mcptoken.Validator, next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenStr := bearerToken(r)
		if tokenStr == "" {
			jsonError(w, http.StatusUnauthorized, "token ausente")
			return
		}

		claims, err := v.Validate(tokenStr)
		if err != nil {
			log.Printf("token inválido: %v", err)
			jsonError(w, http.StatusUnauthorized, "token inválido: "+err.Error())
			return
		}

		log.Printf("acesso autorizado | sub=%s agente=%s scope=%q",
			claims.Subject, claims.AgentSPIFFEID(), claims.Scope)

		next(w, r)
	})
}

// handleListTools retorna a lista de ferramentas disponíveis no MCP Server.
func handleListTools(w http.ResponseWriter, r *http.Request) {
	tools := []map[string]string{
		{"name": "knowledge_search", "description": "Busca semântica na base de conhecimento corporativa"},
		{"name": "document_read", "description": "Leitura de documentos internos por ID"},
		{"name": "policy_check", "description": "Verifica se uma ação está em conformidade com a política IAM"},
	}
	jsonOK(w, map[string]any{"tools": tools})
}

// handleCallTool executa uma ferramenta MCP pelo nome informado no body.
func handleCallTool(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Tool   string         `json:"tool"`
		Params map[string]any `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "body inválido: "+err.Error())
		return
	}

	switch req.Tool {
	case "knowledge_search":
		query, _ := req.Params["query"].(string)
		jsonOK(w, map[string]any{
			"tool":    req.Tool,
			"results": []string{"Resultado simulado para: " + query},
		})
	case "document_read":
		id, _ := req.Params["id"].(string)
		jsonOK(w, map[string]any{
			"tool":    req.Tool,
			"content": "Conteúdo simulado do documento ID=" + id,
		})
	case "policy_check":
		action, _ := req.Params["action"].(string)
		jsonOK(w, map[string]any{
			"tool":    req.Tool,
			"action":  action,
			"allowed": true,
		})
	default:
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
