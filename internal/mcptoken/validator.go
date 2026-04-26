package mcptoken

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

// Validator valida tokens compostos emitidos pelo Auth Server.
// Busca e armazena em cache o JWKS do Auth Server via GET /keys.
type Validator struct {
	mu          sync.RWMutex
	keys        []jose.JSONWebKey
	lastUpdate  time.Time
	jwksURL     string
	expectedAud string
	ttl         time.Duration
}

func NewValidator(jwksURL, expectedAud string) *Validator {
	return &Validator{
		jwksURL:     jwksURL,
		expectedAud: expectedAud,
		ttl:         5 * time.Minute,
	}
}

// Validate valida o token composto e retorna as claims se válido.
func (v *Validator) Validate(tokenStr string) (*CompositeClaims, error) {
	keys, err := v.getKeys()
	if err != nil {
		return nil, fmt.Errorf("falha ao buscar JWKS: %w", err)
	}

	tok, err := jwt.ParseSigned(tokenStr, []jose.SignatureAlgorithm{jose.ES256})
	if err != nil {
		return nil, fmt.Errorf("token malformado: %w", err)
	}

	// Tenta validar contra cada chave do JWKS
	var claims CompositeClaims
	validated := false
	for _, key := range keys {
		if err := tok.Claims(key.Public(), &claims); err == nil {
			validated = true
			break
		}
	}
	if !validated {
		return nil, fmt.Errorf("assinatura inválida")
	}

	// Valida exp, nbf e audience (go-jose/v4: campo renomeado para AnyAudience)
	if err := claims.Claims.ValidateWithLeeway(jwt.Expected{
		AnyAudience: jwt.Audience{v.expectedAud},
	}, 5*time.Second); err != nil {
		return nil, fmt.Errorf("claims inválidas: %w", err)
	}

	if claims.AgentSPIFFEID() == "" {
		return nil, fmt.Errorf("claim act.sub ausente")
	}

	return &claims, nil
}

func (v *Validator) getKeys() ([]jose.JSONWebKey, error) {
	v.mu.RLock()
	if len(v.keys) > 0 && time.Since(v.lastUpdate) < v.ttl {
		keys := v.keys
		v.mu.RUnlock()
		return keys, nil
	}
	v.mu.RUnlock()

	v.mu.Lock()
	defer v.mu.Unlock()

	resp, err := http.Get(v.jwksURL)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", v.jwksURL, err)
	}
	defer resp.Body.Close()

	var keySet jose.JSONWebKeySet
	if err := json.NewDecoder(resp.Body).Decode(&keySet); err != nil {
		return nil, fmt.Errorf("parse JWKS: %w", err)
	}

	v.keys = keySet.Keys
	v.lastUpdate = time.Now()
	return v.keys, nil
}
