package policy

import "strings"

// Policy define as regras de delegação: quais agentes podem atuar em nome de quais usuários.
type Policy struct{}

func New() *Policy { return &Policy{} }

// CanDelegate verifica se o agente pode representar o usuário.
//
// PoC: qualquer agente sob spiffe://empresa.com/agente/* tem permissão.
// Produção: consultar banco de políticas, verificar escopos por recurso, etc.
func (p *Policy) CanDelegate(agentSPIFFEID, _ string) bool {
	return strings.HasPrefix(agentSPIFFEID, "spiffe://empresa.com/agente/")
}
