//go:build e2e

package e2e_test

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// tokenExchangeResponse representa a resposta do endpoint /token do Auth Server.
type tokenExchangeResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

// audience aceita tanto string quanto []string no campo "aud" do JWT (RFC 7519 §4.1.3).
type audience []string

func (a *audience) UnmarshalJSON(data []byte) error {
	// Tenta array primeiro
	var arr []string
	if err := json.Unmarshal(data, &arr); err == nil {
		*a = arr
		return nil
	}
	// Tenta string simples
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	*a = []string{s}
	return nil
}

// compositeClaims representa o payload do composite token (RFC 8693 + RFC 9068).
type compositeClaims struct {
	Issuer   string            `json:"iss"`
	Subject  string            `json:"sub"`
	Audience audience          `json:"aud"`
	Scope    string            `json:"scope"`
	Act      map[string]string `json:"act"`
	ClientID string            `json:"client_id"`
	JTI      string            `json:"jti"`
	Exp      int64             `json:"exp"`
}

// fetchClientJWT obtém um JWT do mock IdP via gen-client-jwt no container auth-server.
func fetchClientJWT(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("docker", "exec", "auth-server", "/gen-client-jwt").Output()
	if err != nil {
		t.Fatalf("falha ao gerar client JWT: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// fetchSVID obtém um JWT SVID do SPIRE Agent via docker exec.
func fetchSVID(t *testing.T) string {
	t.Helper()
	out, err := exec.Command(
		"docker", "exec", "spire-agent",
		"/opt/spire/bin/spire-agent", "api", "fetch", "jwt",
		"-audience", "empresa.com",
		"-socketPath", "/opt/spire/sockets/agent.sock",
	).Output()
	if err != nil {
		t.Fatalf("falha ao buscar SVID JWT: %v\nVerifique se o ambiente está up e registrado.", err)
	}

	// Formato de saída:
	//   token(spiffe://empresa.com/agente/assistente-v2):
	//   \teyJhbGci...
	lines := strings.Split(string(out), "\n")
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "token(") && i+1 < len(lines) {
			svid := strings.TrimSpace(lines[i+1])
			if svid != "" {
				return svid
			}
		}
	}
	t.Fatal("SVID não encontrado na saída do spire-agent. Execute 'make register' primeiro.")
	return ""
}

// doTokenExchange executa o RFC 8693 Token Exchange e retorna o access_token.
func doTokenExchange(t *testing.T, clientJWT, svid string) string {
	t.Helper()

	resp, err := http.PostForm(authServerURL+"/token", url.Values{
		"grant_type":            {"urn:ietf:params:oauth:grant-type:token-exchange"},
		"subject_token":         {clientJWT},
		"subject_token_type":    {"urn:ietf:params:oauth:token-type:jwt"},
		"actor_token":           {svid},
		"actor_token_type":      {"urn:ietf:params:oauth:token-type:jwt"},
		"client_assertion_type": {"urn:ietf:params:oauth:client-assertion-type:jwt-bearer"},
		"client_assertion":      {svid},
		"resource":              {"https://mcp-server.internal/api"},
		"scope":                 {"mcp:tools:read mcp:knowledge:search"},
	})
	if err != nil {
		t.Fatalf("POST /token falhou: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /token retornou %d: %s", resp.StatusCode, body)
	}

	var result tokenExchangeResponse
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("falha ao parsear resposta do token exchange: %v\nbody: %s", err, body)
	}
	if result.AccessToken == "" {
		t.Fatalf("access_token vazio na resposta: %s", body)
	}
	return result.AccessToken
}

// decodeJWTPayload decodifica o payload de um JWT sem verificar assinatura (para inspeção de claims em testes).
func decodeJWTPayload(t *testing.T, token string) compositeClaims {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("token JWT malformado: %d partes", len(parts))
	}
	// Adiciona padding base64url
	payload := parts[1]
	decoded, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		t.Fatalf("falha ao decodificar payload base64: %v", err)
	}
	var claims compositeClaims
	if err := json.Unmarshal(decoded, &claims); err != nil {
		t.Fatalf("falha ao parsear claims JSON: %v", err)
	}
	return claims
}

// callMCPTools faz GET /tools no MCP Server com o token fornecido.
func callMCPTools(t *testing.T, token string) (*http.Response, []byte) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, mcpServerURL+"/tools", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /tools falhou: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp, body
}

// callMCPTool faz POST /tools/call no MCP Server.
func callMCPTool(t *testing.T, token, toolName string, params map[string]any) (*http.Response, map[string]any) {
	t.Helper()
	reqBody, _ := json.Marshal(map[string]any{"tool": toolName, "params": params})
	req, _ := http.NewRequest(http.MethodPost, mcpServerURL+"/tools/call",
		strings.NewReader(string(reqBody)))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /tools/call falhou: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var result map[string]any
	json.Unmarshal(body, &result)
	return resp, result
}

// waitForService aguarda até que o serviço responda com qualquer status HTTP.
// t pode ser nil (chamado de TestMain).
func waitForService(t *testing.T, rawURL string, timeout time.Duration) {
	if t != nil {
		t.Helper()
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(rawURL) //nolint:noctx
		if err == nil {
			resp.Body.Close()
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	msg := fmt.Sprintf("serviço %s não ficou disponível em %s", rawURL, timeout)
	if t != nil {
		t.Fatal(msg)
	} else {
		panic(msg)
	}
}

// mustContainKey verifica que um map contém a chave esperada.
func mustContainKey(t *testing.T, m map[string]any, key string) {
	t.Helper()
	if _, ok := m[key]; !ok {
		t.Errorf("chave %q ausente na resposta: %v", key, m)
	}
}

// mustEqual verifica igualdade com mensagem de erro contextualizada.
func mustEqual(t *testing.T, label string, got, want any) {
	t.Helper()
	if fmt.Sprintf("%v", got) != fmt.Sprintf("%v", want) {
		t.Errorf("%s: got=%v want=%v", label, got, want)
	}
}
