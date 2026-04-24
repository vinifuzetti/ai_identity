package jwks

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"net/http"

	"github.com/go-jose/go-jose/v4"
)

const signingKeyID = "auth-server-key-1"

// SigningKey encapsula o par EC P-256 usado pelo Auth Server para assinar tokens compostos.
type SigningKey struct {
	private *ecdsa.PrivateKey
}

func NewSigningKey() (*SigningKey, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	return &SigningKey{private: key}, nil
}

func (s *SigningKey) Private() *ecdsa.PrivateKey { return s.private }
func (s *SigningKey) KeyID() string              { return signingKeyID }

// ServeJWKS expõe a chave pública do Auth Server em formato JWKS (GET /keys).
// Usada pelo MCP Server para validar tokens compostos.
func (s *SigningKey) ServeJWKS(w http.ResponseWriter, r *http.Request) {
	jwks := jose.JSONWebKeySet{
		Keys: []jose.JSONWebKey{
			{
				Key:       s.private.Public(),
				KeyID:     signingKeyID,
				Algorithm: string(jose.ES256),
				Use:       "sig",
			},
		},
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(jwks)
}
