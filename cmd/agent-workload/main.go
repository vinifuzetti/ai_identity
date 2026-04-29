package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/vinifuzetti/ai_identity/internal/audit"
	ispiffe "github.com/vinifuzetti/ai_identity/internal/spiffe"
)

func main() {
	audit.Init("agent-workload")

	socketPath := env("SPIRE_AGENT_SOCKET", "/opt/spire/sockets/agent.sock")
	clientJWT := env("CLIENT_JWT", "")

	ctx := context.Background()

	if clientJWT == "" {
		// Modo legado: busca e imprime o JWT SVID
		runSVIDDemo(ctx, socketPath)
		return
	}

	// Modo E2E: fluxo completo com Token Exchange + mTLS
	authServerURL := env("AUTH_SERVER_URL", "http://auth-server:8080")
	mcpMTLSURL := env("MCP_SERVER_MTLS_URL", "https://mcp-server:8082")
	runE2EFlow(ctx, socketPath, authServerURL, mcpMTLSURL, clientJWT)
}

// runSVIDDemo busca e imprime o JWT SVID (comportamento original).
func runSVIDDemo(ctx context.Context, socketPath string) {
	client, err := ispiffe.NewClient(ctx, socketPath)
	if err != nil {
		slog.Error("falha ao conectar à Workload API", "error", err)
		os.Exit(1)
	}
	defer client.Close()

	svid, err := client.FetchJWTSVID(ctx, env("SVID_AUDIENCE", "empresa.com"))
	if err != nil {
		slog.Error("falha ao buscar JWT SVID", "error", err)
		os.Exit(1)
	}

	fmt.Printf("SPIFFE ID : %s\n", svid.ID)
	fmt.Printf("Audience  : %v\n", svid.Audience)
	fmt.Printf("JWT       : %s\n", svid.Marshal())
}

// runE2EFlow executa o fluxo completo:
//  1. Obtém JWT SVID via Workload API
//  2. Executa Token Exchange (RFC 8693) → composite token
//  3. Abre conexão mTLS com X.509-SVID ao MCP Server
//  4. Chama ferramentas com composite token no header Authorization
func runE2EFlow(ctx context.Context, socketPath, authServerURL, mcpMTLSURL, clientJWT string) {
	slog.Info("iniciando fluxo E2E", "auth_server", authServerURL, "mcp_server", mcpMTLSURL)

	// 1. JWT SVID para o token exchange
	wl, err := ispiffe.NewClient(ctx, socketPath)
	if err != nil {
		slog.Error("falha ao conectar à Workload API", "error", err)
		os.Exit(1)
	}
	defer wl.Close()

	jwtSVID, err := wl.FetchJWTSVID(ctx, "empresa.com")
	if err != nil {
		slog.Error("falha ao buscar JWT SVID", "error", err)
		os.Exit(1)
	}
	slog.Info("JWT SVID obtido", "spiffe_id", jwtSVID.ID.String())

	// 2. Token Exchange → composite token
	compositeToken, err := doTokenExchange(authServerURL, clientJWT, jwtSVID.Marshal())
	if err != nil {
		slog.Error("token exchange falhou", "error", err)
		os.Exit(1)
	}
	slog.Info("composite token obtido via token exchange (RFC 8693)")

	// 3. X.509 Source para mTLS — usa o SVID do agente
	x509Source, err := ispiffe.NewX509Source(ctx, socketPath,
		"spiffe://empresa.com/agente/assistente-v2")
	if err != nil {
		slog.Error("falha ao criar X509Source", "error", err)
		os.Exit(1)
	}
	defer x509Source.Close()

	x509SVID, _ := x509Source.GetX509SVID()
	slog.Info("X.509 SVID obtido para mTLS", "spiffe_id", x509SVID.ID.String())

	// 4. Cliente HTTP com mTLS — autentica apenas o MCP Server com SPIFFE ID correto
	tlsConfig, err := ispiffe.MTLSClientConfig(x509Source, "spiffe://empresa.com/mcp/tools-server")
	if err != nil {
		slog.Error("falha ao criar mTLS config", "error", err)
		os.Exit(1)
	}
	httpClient := &http.Client{
		Transport: &http.Transport{TLSClientConfig: tlsConfig},
	}

	slog.Info("conexão mTLS estabelecida com MCP Server")

	// 5. GET /tools via mTLS
	tools, err := listTools(httpClient, mcpMTLSURL, compositeToken)
	if err != nil {
		slog.Error("falha ao listar ferramentas", "error", err)
		os.Exit(1)
	}
	slog.Info("ferramentas disponíveis via mTLS", "tools", tools)

	// 6. POST /tools/call via mTLS
	result, err := callTool(httpClient, mcpMTLSURL, compositeToken,
		"knowledge_search", map[string]any{"query": "política de acesso IAM"})
	if err != nil {
		slog.Error("falha ao chamar ferramenta", "error", err)
		os.Exit(1)
	}
	slog.Info("ferramenta chamada com sucesso via mTLS", "result", result)

	slog.Info("fluxo E2E com mTLS concluído")
}

// doTokenExchange executa o RFC 8693 Token Exchange e retorna o access_token.
func doTokenExchange(authServerURL, clientJWT, svid string) (string, error) {
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
		return "", fmt.Errorf("POST /token: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token exchange retornou %d: %s", resp.StatusCode, body)
	}

	var result struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}
	return result.AccessToken, nil
}

// listTools chama GET /tools com o token composto.
func listTools(client *http.Client, baseURL, token string) ([]string, error) {
	req, _ := http.NewRequest(http.MethodGet, baseURL+"/tools", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET /tools: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	names := make([]string, len(result.Tools))
	for i, t := range result.Tools {
		names[i] = t.Name
	}
	return names, nil
}

// callTool chama POST /tools/call com o token composto.
func callTool(client *http.Client, baseURL, token, toolName string, params map[string]any) (map[string]any, error) {
	body, _ := json.Marshal(map[string]any{"tool": toolName, "params": params})
	req, _ := http.NewRequest(http.MethodPost, baseURL+"/tools/call",
		strings.NewReader(string(body)))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("POST /tools/call: %w", err)
	}
	defer resp.Body.Close()

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	return result, nil
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
