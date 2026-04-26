package mcptoken

import "github.com/go-jose/go-jose/v4/jwt"

// CompositeClaims representa o payload do token composto emitido pelo Auth Server.
// Referência: RFC 8693 §4.1 (act claim) e RFC 9068 (access token profile).
type CompositeClaims struct {
	jwt.Claims
	Scope    string            `json:"scope"`
	Act      map[string]string `json:"act"`
	ClientID string            `json:"client_id"`
	JTI      string            `json:"jti"`
}

// AgentSPIFFEID retorna o SPIFFE ID do agente a partir do claim `act.sub`.
func (c *CompositeClaims) AgentSPIFFEID() string {
	if c.Act != nil {
		return c.Act["sub"]
	}
	return ""
}
