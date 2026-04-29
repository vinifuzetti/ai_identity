package tokenexchange

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/spiffe/go-spiffe/v2/bundle/jwtbundle"
	"github.com/spiffe/go-spiffe/v2/svid/jwtsvid"
	"github.com/vinifuzetti/ai_identity/auth-server/internal/audit"
	"github.com/vinifuzetti/ai_identity/auth-server/internal/jwks"
	"github.com/vinifuzetti/ai_identity/auth-server/internal/policy"
)

const (
	grantTypeTokenExchange = "urn:ietf:params:oauth:grant-type:token-exchange"
	issuer                 = "https://auth.empresa.com"
	defaultAudience        = "https://mcp-server.internal/api"
	svidAudience           = "empresa.com"
	tokenTTL               = 5 * time.Minute
)

// compositeClaims representa o payload do token composto emitido pelo Auth Server.
// Referência: RFC 8693 §4.1 (act claim) e RFC 9068 (access token profile).
type compositeClaims struct {
	jwt.Claims
	Scope    string            `json:"scope,omitempty"`
	Act      map[string]string `json:"act"`
	ClientID string            `json:"client_id"`
	JTI      string            `json:"jti"`
}

type tokenResponse struct {
	AccessToken     string `json:"access_token"`
	TokenType       string `json:"token_type"`
	ExpiresIn       int    `json:"expires_in"`
	IssuedTokenType string `json:"issued_token_type"`
}

type errorResponse struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description,omitempty"`
}

// Handler implementa o endpoint POST /token (RFC 8693 — OAuth 2.0 Token Exchange).
type Handler struct {
	idpKey       *ecdsa.PublicKey
	signingKey   *jwks.SigningKey
	bundleSource jwtbundle.Source
	policy       *policy.Policy
}

func NewHandler(idpKey *ecdsa.PublicKey, sk *jwks.SigningKey, bundleSource jwtbundle.Source, pol *policy.Policy) *Handler {
	return &Handler{idpKey: idpKey, signingKey: sk, bundleSource: bundleSource, policy: pol}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ip := r.RemoteAddr

	if err := r.ParseForm(); err != nil {
		audit.TokenExchangeDenied("parse_error", ip, err)
		writeError(w, http.StatusBadRequest, "invalid_request", "falha ao parsear body")
		return
	}

	if r.FormValue("grant_type") != grantTypeTokenExchange {
		audit.TokenExchangeDenied("unsupported_grant_type", ip, nil)
		writeError(w, http.StatusBadRequest, "unsupported_grant_type", "")
		return
	}

	subjectToken := r.FormValue("subject_token")
	actorToken := r.FormValue("actor_token")
	clientAssertion := r.FormValue("client_assertion")
	resource := r.FormValue("resource")
	scope := r.FormValue("scope")

	if subjectToken == "" || actorToken == "" || clientAssertion == "" {
		audit.TokenExchangeDenied("missing_required_params", ip, nil)
		writeError(w, http.StatusBadRequest, "invalid_request", "subject_token, actor_token e client_assertion são obrigatórios")
		return
	}
	if resource == "" {
		resource = defaultAudience
	}

	ctx := r.Context()

	// 1. Autenticar cliente OAuth via JWT Bearer Assertion — RFC 7523
	agentSVID, err := h.validateSVID(ctx, clientAssertion)
	if err != nil {
		audit.TokenExchangeDenied("invalid_client_assertion", ip, err)
		writeError(w, http.StatusUnauthorized, "invalid_client", "client_assertion inválido: "+err.Error())
		return
	}

	// 2. Validar subject_token — JWT do usuário emitido pelo IdP corporativo
	userSubject, err := h.validateSubjectToken(subjectToken)
	if err != nil {
		audit.TokenExchangeDenied("invalid_subject_token", ip, err)
		writeError(w, http.StatusBadRequest, "invalid_request", "subject_token inválido: "+err.Error())
		return
	}

	// 3. Validar actor_token — SVID JWT do agente
	actorSVID, err := h.validateSVID(ctx, actorToken)
	if err != nil {
		audit.TokenExchangeDenied("invalid_actor_token", ip, err)
		writeError(w, http.StatusBadRequest, "invalid_request", "actor_token inválido: "+err.Error())
		return
	}

	// client_assertion e actor_token devem representar a mesma identidade
	if agentSVID.ID.String() != actorSVID.ID.String() {
		audit.TokenExchangeDenied("identity_mismatch", ip, nil)
		writeError(w, http.StatusBadRequest, "invalid_request", "client_assertion e actor_token com identidades diferentes")
		return
	}

	// 4. Política de delegação
	if !h.policy.CanDelegate(agentSVID.ID.String(), userSubject) {
		audit.TokenExchangeDenied("delegation_denied", ip, nil)
		writeError(w, http.StatusForbidden, "access_denied", "delegação não permitida pela política")
		return
	}

	// 5. Emitir token composto
	token, jti, err := h.issueCompositeToken(userSubject, agentSVID.ID.String(), resource, scope)
	if err != nil {
		audit.TokenExchangeError("issue_failed", ip, err)
		writeError(w, http.StatusInternalServerError, "server_error", "falha ao emitir token composto")
		return
	}

	audit.TokenExchangeSuccess(userSubject, agentSVID.ID.String(), jti, resource, scope, ip)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tokenResponse{
		AccessToken:     token,
		TokenType:       "Bearer",
		ExpiresIn:       int(tokenTTL.Seconds()),
		IssuedTokenType: "urn:ietf:params:oauth:token-type:jwt",
	})
}

// ServeJWKS expõe a chave pública do Auth Server (GET /keys).
func (h *Handler) ServeJWKS(w http.ResponseWriter, r *http.Request) {
	h.signingKey.ServeJWKS(w, r)
}

// validateSVID valida um JWT SVID contra o bundle JWKS exportado pelo SPIRE Server.
func (h *Handler) validateSVID(_ context.Context, token string) (*jwtsvid.SVID, error) {
	return jwtsvid.ParseAndValidate(token, h.bundleSource, []string{svidAudience})
}

// validateSubjectToken valida o JWT do usuário contra a chave pública do IdP.
// Retorna o subject (sub) do token.
func (h *Handler) validateSubjectToken(token string) (string, error) {
	parsed, err := jwt.ParseSigned(token, []jose.SignatureAlgorithm{jose.ES256})
	if err != nil {
		return "", fmt.Errorf("parse JWT: %w", err)
	}

	var claims jwt.Claims
	if err := parsed.Claims(h.idpKey, &claims); err != nil {
		return "", fmt.Errorf("verificação de assinatura: %w", err)
	}

	if err := claims.ValidateWithLeeway(jwt.Expected{}, time.Minute); err != nil {
		return "", fmt.Errorf("claims inválidas: %w", err)
	}

	return claims.Subject, nil
}

// issueCompositeToken emite o token composto com delegação (act claim — RFC 8693 §4.1).
// Retorna o token serializado e o jti para correlação nos audit logs.
func (h *Handler) issueCompositeToken(userSub, agentSPIFFEID, audience, scope string) (token, jti string, err error) {
	sig, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.ES256, Key: h.signingKey.Private()},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", h.signingKey.KeyID()),
	)
	if err != nil {
		return "", "", err
	}

	jti = generateJTI()
	now := time.Now()
	claims := compositeClaims{
		Claims: jwt.Claims{
			Issuer:   issuer,
			Subject:  userSub,
			Audience: jwt.Audience{audience},
			IssuedAt: jwt.NewNumericDate(now),
			Expiry:   jwt.NewNumericDate(now.Add(tokenTTL)),
		},
		Scope:    scope,
		Act:      map[string]string{"sub": agentSPIFFEID},
		ClientID: agentSPIFFEID,
		JTI:      jti,
	}

	token, err = jwt.Signed(sig).Claims(claims).Serialize()
	return token, jti, err
}

func generateJTI() string {
	b := make([]byte, 8)
	rand.Read(b)
	return fmt.Sprintf("txn-%x", b)
}

func writeError(w http.ResponseWriter, status int, code, description string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(errorResponse{Error: code, ErrorDescription: description})
}
