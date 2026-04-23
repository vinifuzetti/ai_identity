# PoC: IAM para Agentes de IA com SPIFFE + Token Exchange + MCP

## Objetivo

Prova de conceito para arquitetura de IAM de agentes de IA em ambiente corporativo, baseada no Internet-Draft IETF `draft-klrc-aiagent-auth-00` (março 2026). A PoC demonstra como um agente de IA que atende um cliente logado pode acessar tools em um MCP Server com identidade verificável e delegação preservada.

## Cenário

Um cliente autenticado envia seu JWT para um agente de IA. O agente precisa acessar tools expostas por um MCP Server. O acesso do agente ao MCP Server é mediado por um Authorization Server que executa OAuth 2.0 Token Exchange (RFC 8693), produzindo um token composto que carrega tanto o contexto do usuário quanto a identidade do agente.

A identidade do agente (e de todos os workloads de infraestrutura) é gerenciada via SPIFFE/SPIRE, com SVIDs JWT de curta duração emitidos sob o trust domain `empresa.com`.

## Componentes

### Cliente
- Possui JWT emitido por um IdP corporativo
- Envia JWT ao agente no início da interação
- Não conhece SPIFFE nem MCP

### SPIRE Server
- CA da infraestrutura de workload identity
- Trust domain: `spiffe://empresa.com`
- Expõe JWKS para validação de SVIDs JWT
- Em PoC: SQLite + memory keymanager + join_token NodeAttestor
- Em produção: Postgres + disk/HSM keymanager + k8s_psat/aws_iid

### SPIRE Agent
- Daemon por nó
- Entrega SVID ao workload via Unix domain socket
- Rotaciona SVIDs automaticamente

### Agente de IA
- Workload com SPIFFE ID `spiffe://empresa.com/agente/assistente-v2`
- Recebe JWT do cliente
- Obtém SVID JWT via Workload API do SPIRE Agent
- Executa Token Exchange no Authorization Server
- Chama MCP Server com token composto

### Authorization Server
- Implementa OAuth 2.0 Token Exchange (RFC 8693)
- Valida JWT do cliente contra JWKS do IdP
- Valida SVID do agente contra JWKS do SPIRE Server (via SPIRE Agent local)
- Autentica o cliente OAuth (o agente) via JWT Bearer Assertion (RFC 7523)
- Aplica políticas de delegação (este agente pode atuar por este usuário?)
- Emite token composto com claim `act.sub` preservando a identidade do agente

### MCP Server
- Possui SPIFFE ID próprio `spiffe://empresa.com/mcp/tools-server`
- Valida token composto via JWKS do Authorization Server
- Extrai `sub` (usuário) e `act.sub` (agente) para decisões de autorização
- Registra audit logs com correlação via `jti`

## Fluxo de Token Exchange

### Requisição
```
POST /token
Content-Type: application/x-www-form-urlencoded

grant_type=urn:ietf:params:oauth:grant-type:token-exchange
subject_token=<JWT do cliente>
subject_token_type=urn:ietf:params:oauth:token-type:jwt
actor_token=<SVID JWT do agente>
actor_token_type=urn:ietf:params:oauth:token-type:jwt
resource=https://mcp-server.internal/api
scope=mcp:tools:read mcp:knowledge:search
client_assertion_type=urn:ietf:params:oauth:client-assertion-type:jwt-bearer
client_assertion=<mesmo SVID JWT, usado para autenticar o cliente OAuth>
```

### Token composto emitido
```json
{
  "iss": "https://auth.empresa.com",
  "sub": "user-8f3a2c",
  "aud": "https://mcp-server.internal/api",
  "exp": 1745276700,
  "scope": "mcp:tools:read mcp:knowledge:search",
  "act": { "sub": "spiffe://empresa.com/agente/assistente-v2" },
  "client_id": "spiffe://empresa.com/agente/assistente-v2",
  "jti": "txn-9d4e1f82"
}
```

## Decisões arquiteturais

1. **Go como linguagem principal** — SDK `go-spiffe/v2` é o mais maduro; ecossistema OAuth robusto (`fosite`, `golang-jwt/jwt`).

2. **SVID como actor_token E como client_assertion** — o SVID tem papel duplo: representa quem está agindo (actor_token na semântica de delegação) e autentica o cliente OAuth (client_assertion conforme RFC 7523). Isso evita credenciais adicionais para o agente.

3. **MCP Server também tem SPIFFE ID** — defesa em profundidade: mTLS entre agente e MCP Server além da validação do token composto.

4. **JWKS do SPIRE via SPIRE Agent local** — o Authorization Server roda um SPIRE Agent no mesmo pod/host para buscar o bundle do trust domain. Evita URLs HTTP diretas ao SPIRE Server.

5. **Tokens compostos de curta duração (5 min)** — o agente solicita novo token por interação relevante. Reduz janela de replay.

6. **Audience restrita** — cada token composto é emitido para um MCP Server específico via parâmetro `resource`. Defesa contra confused deputy.

## Antipadrões evitados

- Agente nunca repassa o JWT original do cliente ao MCP Server
- MCP Server nunca recebe o SVID bruto do agente
- Nenhum componente usa API keys estáticas
- Credenciais do workload nunca são de longa duração

## Estrutura de projeto sugerida

O Authorization Server é um módulo Go separado dentro do monorepo — tem roadmap próprio
(introspection, revogação, PKCE, device flow) e pode ser extraído para repositório independente
sem refatoração.

```
ai_identity/
├── cmd/
│   ├── agent-workload/      # simulador do agente de IA
│   └── mcp-server/          # MCP Server com validação de token
├── internal/
│   ├── spiffe/              # helpers go-spiffe
│   ├── jwks/                # cache de JWKS
│   └── policy/              # políticas de delegação
├── auth-server/             # módulo Go independente (go.mod próprio)
│   ├── cmd/server/          # entrypoint do Authorization Server
│   └── internal/
│       ├── tokenexchange/   # lógica RFC 8693
│       ├── jwks/            # cache de JWKS
│       └── policy/          # políticas de delegação
├── config/
│   ├── server/server.conf
│   └── agent/agent.conf
├── docker/
├── scripts/
├── test/
│   ├── e2e/                 # testes de fluxo completo
│   └── fixtures/            # JWTs de teste
├── docker-compose.yml
├── Makefile
├── go.mod                   # módulo principal (agent-workload + mcp-server)
└── ARCHITECTURE.md
```

## Referências

- Internet-Draft IETF: `draft-klrc-aiagent-auth-00`
- RFC 8693: OAuth 2.0 Token Exchange
- RFC 7523: JWT Profile for OAuth 2.0 Client Authentication
- RFC 9068: JWT Profile for OAuth 2.0 Access Tokens
- SPIFFE/SPIRE: https://spiffe.io/docs/latest/
- go-spiffe: https://github.com/spiffe/go-spiffe

## Tarefas da PoC (roadmap sugerido)

1. Subir SPIRE Server + Agent com docker-compose e validar emissão de SVID manualmente
2. Implementar agent-workload que busca SVID via Workload API
3. Implementar Authorization Server com endpoint /token exchange
4. Implementar MCP Server com validação de token composto
5. Integrar fluxo end-to-end com teste automatizado
6. Adicionar mTLS entre agente e MCP Server (SPIFFE X.509-SVID)
7. Adicionar audit logging estruturado
8. Documentar extensão para ambiente Kubernetes com k8s_psat
