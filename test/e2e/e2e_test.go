//go:build e2e

// Package e2e contém testes de integração end-to-end que validam o fluxo completo:
//
//	SPIRE Agent → Token Exchange (RFC 8693) → MCP Server
//
// Pré-requisitos: ambiente rodando (`make up && make register`).
// Execução: make test-e2e
package e2e_test

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

var (
	authServerURL = envOr("AUTH_SERVER_URL", "http://localhost:8080")
	mcpServerURL  = envOr("MCP_SERVER_URL", "http://localhost:8081")
)

// TestMain verifica disponibilidade dos serviços antes de rodar os testes.
func TestMain(m *testing.M) {
	waitForService(nil, authServerURL+"/keys", 15*time.Second)
	// /tools retorna 401 sem token mas a conexão TCP é estabelecida — serviço está up.
	waitForService(nil, mcpServerURL+"/tools", 15*time.Second)
	os.Exit(m.Run())
}

// ---------------------------------------------------------------------------
// Token Exchange (Auth Server)
// ---------------------------------------------------------------------------

// TestTokenExchange_Sucesso valida o fluxo completo de token exchange RFC 8693.
func TestTokenExchange_Sucesso(t *testing.T) {
	clientJWT := fetchClientJWT(t)
	svid := fetchSVID(t)

	compositeToken := doTokenExchange(t, clientJWT, svid)

	if compositeToken == "" {
		t.Fatal("composite token vazio")
	}
	if strings.Count(compositeToken, ".") != 2 {
		t.Fatalf("composite token não é um JWT válido (esperado 3 partes): %s", compositeToken[:80])
	}
}

// TestTokenExchange_Claims valida as claims do composite token emitido.
func TestTokenExchange_Claims(t *testing.T) {
	clientJWT := fetchClientJWT(t)
	svid := fetchSVID(t)
	compositeToken := doTokenExchange(t, clientJWT, svid)

	claims := decodeJWTPayload(t, compositeToken)

	// sub deve ser o usuário do cliente JWT
	if claims.Subject == "" {
		t.Error("claim 'sub' ausente no composite token")
	}

	// act.sub deve ser o SPIFFE ID do agente (RFC 8693 §4.1)
	if claims.Act == nil || claims.Act["sub"] == "" {
		t.Error("claim 'act.sub' ausente — identidade do agente não preservada")
	}
	if !strings.HasPrefix(claims.Act["sub"], "spiffe://empresa.com/agente/") {
		t.Errorf("act.sub inválido: %s", claims.Act["sub"])
	}

	// aud deve ser o MCP Server
	found := false
	for _, a := range claims.Audience {
		if a == "https://mcp-server.internal/api" {
			found = true
		}
	}
	if !found {
		t.Errorf("audience esperada não encontrada: %v", claims.Audience)
	}

	// scope deve conter os escopos solicitados
	for _, scope := range []string{"mcp:tools:read", "mcp:knowledge:search"} {
		if !strings.Contains(claims.Scope, scope) {
			t.Errorf("scope %q ausente: %s", scope, claims.Scope)
		}
	}

	// jti deve ser não vazio (rastreabilidade)
	if claims.JTI == "" {
		t.Error("claim 'jti' ausente — rastreabilidade comprometida")
	}

	// exp deve ser no futuro
	if claims.Exp <= time.Now().Unix() {
		t.Errorf("composite token já expirado: exp=%d now=%d", claims.Exp, time.Now().Unix())
	}

	t.Logf("sub=%s act.sub=%s scope=%q jti=%s", claims.Subject, claims.Act["sub"], claims.Scope, claims.JTI)
}

// TestTokenExchange_SemSVID verifica que o exchange falha sem actor_token.
func TestTokenExchange_SemSVID(t *testing.T) {
	clientJWT := fetchClientJWT(t)

	resp, err := http.PostForm(authServerURL+"/token", map[string][]string{
		"grant_type":         {"urn:ietf:params:oauth:grant-type:token-exchange"},
		"subject_token":      {clientJWT},
		"subject_token_type": {"urn:ietf:params:oauth:token-type:jwt"},
		// actor_token e client_assertion ausentes
	})
	if err != nil {
		t.Fatalf("POST /token falhou: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		t.Error("esperado erro (4xx), mas token exchange foi aceito sem SVID")
	}
}

// ---------------------------------------------------------------------------
// MCP Server — autenticação
// ---------------------------------------------------------------------------

// TestMCPServer_SemToken verifica que GET /tools retorna 401 sem Authorization.
func TestMCPServer_SemToken(t *testing.T) {
	resp, body := callMCPTools(t, "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("esperado 401, got %d: %s", resp.StatusCode, body)
	}
}

// TestMCPServer_TokenInvalido verifica que token aleatório é rejeitado com 401.
func TestMCPServer_TokenInvalido(t *testing.T) {
	resp, body := callMCPTools(t, "eyJhbGciOiJFUzI1NiJ9.invalido.invalido")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("esperado 401, got %d: %s", resp.StatusCode, body)
	}
}

// ---------------------------------------------------------------------------
// MCP Server — ferramentas
// ---------------------------------------------------------------------------

// TestMCPServer_ListTools valida a lista de ferramentas disponíveis.
func TestMCPServer_ListTools(t *testing.T) {
	token := doTokenExchange(t, fetchClientJWT(t), fetchSVID(t))

	resp, body := callMCPTools(t, token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("esperado 200, got %d: %s", resp.StatusCode, body)
	}

	var result struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("falha ao parsear lista de tools: %v", err)
	}

	toolNames := make(map[string]bool)
	for _, tool := range result.Tools {
		toolNames[tool.Name] = true
	}

	for _, expected := range []string{"knowledge_search", "document_read", "policy_check"} {
		if !toolNames[expected] {
			t.Errorf("ferramenta %q ausente na lista: %v", expected, toolNames)
		}
	}

	t.Logf("%d ferramentas: %v", len(result.Tools), toolNames)
}

// TestMCPServer_KnowledgeSearch valida a chamada da ferramenta knowledge_search.
func TestMCPServer_KnowledgeSearch(t *testing.T) {
	token := doTokenExchange(t, fetchClientJWT(t), fetchSVID(t))

	resp, result := callMCPTool(t, token, "knowledge_search", map[string]any{
		"query": "política de acesso IAM",
	})

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("esperado 200, got %d: %v", resp.StatusCode, result)
	}
	mustContainKey(t, result, "results")
	mustEqual(t, "tool", result["tool"], "knowledge_search")
}

// TestMCPServer_DocumentRead valida a chamada da ferramenta document_read.
func TestMCPServer_DocumentRead(t *testing.T) {
	token := doTokenExchange(t, fetchClientJWT(t), fetchSVID(t))

	resp, result := callMCPTool(t, token, "document_read", map[string]any{
		"id": "doc-001",
	})

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("esperado 200, got %d: %v", resp.StatusCode, result)
	}
	mustContainKey(t, result, "content")
	mustEqual(t, "tool", result["tool"], "document_read")
}

// TestMCPServer_PolicyCheck valida a chamada da ferramenta policy_check.
func TestMCPServer_PolicyCheck(t *testing.T) {
	token := doTokenExchange(t, fetchClientJWT(t), fetchSVID(t))

	resp, result := callMCPTool(t, token, "policy_check", map[string]any{
		"action": "document:read",
	})

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("esperado 200, got %d: %v", resp.StatusCode, result)
	}
	mustEqual(t, "tool", result["tool"], "policy_check")
	mustEqual(t, "allowed", result["allowed"], true)
}

// TestMCPServer_FerramentaDesconhecida verifica 404 para tool inexistente.
func TestMCPServer_FerramentaDesconhecida(t *testing.T) {
	token := doTokenExchange(t, fetchClientJWT(t), fetchSVID(t))

	resp, _ := callMCPTool(t, token, "ferramenta_inexistente", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("esperado 404, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
