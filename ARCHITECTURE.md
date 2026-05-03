# Arquitetura: IAM para Agentes de IA com SPIFFE + Token Exchange + MCP

## Objetivo

Definir uma arquitetura de identidade e acesso para agentes de IA em ambiente corporativo, baseada no Internet-Draft IETF `draft-klrc-aiagent-auth-00`. O objetivo central é garantir que toda requisição de um agente de IA a um recurso corporativo seja criptograficamente atribuível a uma identidade de workload verificável **e** preserve o contexto do usuário que iniciou a interação.

---

## Cenário

Um usuário autenticado interage com um agente de IA. O agente precisa acessar ferramentas expostas por um MCP Server (busca, leitura de documentos, verificação de políticas). O acesso deve ser:

- **Autenticado**: o agente prova sua identidade com uma credencial criptográfica emitida pela infraestrutura (não configurada manualmente)
- **Delegado**: o token de acesso preserva quem é o usuário (`sub`) e quem é o agente (`act.sub`) — rastreável até o recurso final
- **Auditável**: cada operação gera um evento estruturado com correlação entre serviços via `jti`
- **Defesa em profundidade**: o transporte é protegido por mTLS com SVIDs X.509, além da validação do token na camada de aplicação

---

## Componentes

### Cliente / Usuário
- Possui JWT emitido por um IdP corporativo (OIDC, Azure AD, Okta, etc.)
- Entrega o JWT ao agente no início da interação
- Não conhece SPIFFE, SPIRE, nem MCP — a complexidade de identidade é transparente para o usuário

### SPIRE Server
- CA da infraestrutura de workload identity; trust domain `spiffe://empresa.com`
- Emite SVIDs X.509 (para mTLS) e JWT (para token exchange) aos workloads
- Exporta o bundle JWKS periodicamente para um volume compartilhado — o Auth Server lê desse arquivo, sem necessidade de SPIRE Agent co-localizado
- **PoC**: SQLite + memory keymanager + `join_token` NodeAttestor
- **Produção**: Postgres + disk/HSM keymanager + `k8s_psat` / `aws_iid`

### SPIRE Agent
- Daemon por nó; conecta ao SPIRE Server via gRPC mTLS no bootstrap
- Expõe a Workload API via Unix domain socket (`agent.sock`)
- Atesta workloads por seletores (PoC: `unix:uid`) e entrega SVIDs de curta duração
- Rotaciona SVIDs automaticamente; workloads nunca precisam gerenciar ciclo de vida de certificados

### Agente de IA (`agent-workload`)
- SPIFFE ID: `spiffe://empresa.com/agente/assistente-v2` (uid 0)
- Recebe o JWT do usuário como entrada
- Busca JWT SVID via Workload API para o token exchange
- Executa Token Exchange no Authorization Server → obtém composite token
- Obtém X.509 SVID via Workload API para o transporte mTLS
- Abre conexão mTLS com o MCP Server e apresenta o composite token no header `Authorization`

### Authorization Server
- Implementa [RFC 8693](https://www.rfc-editor.org/rfc/rfc8693) — OAuth 2.0 Token Exchange
- Endpoint: `POST /token`
- Valida o JWT do usuário contra a chave pública do IdP (EC P-256)
- Valida o SVID JWT do agente contra o bundle JWKS do SPIRE (arquivo compartilhado)
- Autentica o cliente OAuth via JWT Bearer Assertion ([RFC 7523](https://www.rfc-editor.org/rfc/rfc7523)) — o mesmo SVID serve como `actor_token` e `client_assertion`
- Aplica política de delegação: agentes com prefixo `spiffe://empresa.com/agente/` podem delegar por qualquer usuário
- Emite composite token com TTL 5 minutos, assinado com chave EC P-256 efêmera (gerada no startup)
- Expõe `GET /keys` (JWKS público) para que o MCP Server valide os tokens emitidos

### MCP Server
- SPIFFE ID: `spiffe://empresa.com/mcp/tools-server` (uid 1000 — seletor distinto do agente)
- Duas camadas de autenticação:
  1. **Transporte**: listener mTLS em `:8082` — exige X.509 SVID válido do trust domain `empresa.com`
  2. **Aplicação**: valida o composite token Bearer via JWKS do Authorization Server (`GET /keys`)
- Extrai `sub` (usuário) e `act.sub` (agente) para decisões de autorização granular
- Expõe ferramentas: `knowledge_search`, `document_read`, `policy_check`
- Mantém listener HTTP em `:8081` para ambiente de desenvolvimento e testes automatizados

---

## Fluxo completo

```
Usuário                  Agente de IA             Auth Server         MCP Server
   |                          |                        |                   |
   |-- ① JWT do usuário ------>|                        |                   |
   |                          |-- ② FetchJWTSVID ------>|                   |
   |                          |<-- JWT SVID ------------|                   |
   |                          |                        |                   |
   |                          |-- ③ POST /token ------->|                   |
   |                          |   subject_token (JWT)   |                   |
   |                          |   actor_token (SVID)    |                   |
   |                          |   client_assertion(SVID)|                   |
   |                          |<-- composite token -----|                   |
   |                          |   sub · act.sub · jti   |                   |
   |                          |                        |                   |
   |                          |-- ④ FetchX509SVID ----->|                   |
   |                          |<-- X.509 SVID ----------|                   |
   |                          |                        |                   |
   |                          |-- ⑤ mTLS handshake (X.509 SVIDs) ---------->|
   |                          |-- Bearer composite token ------------------>|
   |                          |<-- resposta da ferramenta -----------------|
```

---

## Token Exchange (RFC 8693)

### Requisição

```
POST /token
Content-Type: application/x-www-form-urlencoded

grant_type             = urn:ietf:params:oauth:grant-type:token-exchange
subject_token          = <JWT do usuário emitido pelo IdP>
subject_token_type     = urn:ietf:params:oauth:token-type:jwt
actor_token            = <JWT SVID do agente>
actor_token_type       = urn:ietf:params:oauth:token-type:jwt
client_assertion_type  = urn:ietf:params:oauth:client-assertion-type:jwt-bearer
client_assertion       = <mesmo JWT SVID — duplo papel: actor + client auth>
resource               = https://mcp-server.internal/api
scope                  = mcp:tools:read mcp:knowledge:search
```

### Composite token emitido

```json
{
  "iss": "https://auth.empresa.com",
  "sub": "user-8f3a2c",
  "aud": "https://mcp-server.internal/api",
  "exp": 1745276700,
  "iat": 1745276400,
  "scope": "mcp:tools:read mcp:knowledge:search",
  "act": { "sub": "spiffe://empresa.com/agente/assistente-v2" },
  "client_id": "spiffe://empresa.com/agente/assistente-v2",
  "jti": "txn-9d4e1f82"
}
```

---

## Audit logging

Cada serviço emite eventos JSON no stdout com campos consistentes para ingestão por SIEM.

### Eventos do Authorization Server

| Evento | Level | Quando |
|---|---|---|
| `token_exchange_success` | INFO | Exchange concluído com sucesso |
| `token_exchange_denied` | WARN | SVID inválido, policy negada, parâmetros ausentes |
| `token_exchange_error` | ERROR | Falha interna ao emitir token |

### Eventos do MCP Server

| Evento | Level | Quando |
|---|---|---|
| `mcp_access_authorized` | INFO | Token válido aceito no middleware |
| `mcp_access_denied` | WARN | Ausência ou invalidade do token |
| `mcp_tool_called` | INFO | Ferramenta invocada (sucesso ou erro) |

### Campos comuns

```
service   — "auth-server" | "mcp-server"
event     — nome do evento
status    — "success" | "denied" | "error"
sub       — usuário (do composite token)
agent     — SPIFFE ID do agente (act.sub)
jti       — ID da transação — correlaciona eventos entre serviços
```

O `jti` permite reconstruir o ciclo de vida completo de uma transação com uma única query:
```
jti = "txn-cec74237d99c3bc4" → token_exchange_success + mcp_access_authorized + mcp_tool_called
```

---

## Decisões arquiteturais

### 1. SVID com papel duplo: `actor_token` + `client_assertion`
O JWT SVID do agente serve tanto como evidência de delegação (RFC 8693 `actor_token`) quanto como credencial de autenticação do cliente OAuth (RFC 7523 `client_assertion`). Isso elimina a necessidade de credenciais adicionais para o agente e mantém a prova de identidade em um único artefato criptográfico.

### 2. Auth Server sem SPIRE Agent co-localizado
O bundle JWKS do SPIRE (chaves de verificação públicas do trust domain) é exportado periodicamente pelo SPIRE Server para um volume compartilhado. O Auth Server lê desse arquivo a cada requisição (com cache de 60s). Isso elimina a dependência de infraestrutura de workload identity no Auth Server, que pode ser deployado como um serviço stateless.

### 3. Identidades separadas por uid no Docker
O `mcp-server` roda como `mcpserver` (uid 1000) e o `agent-workload` como root (uid 0). O SPIRE Agent usa seletores `unix:uid` para atribuir SPIFFE IDs distintos a cada processo, sem necessidade de attestors externos. Em produção, usar `k8s_psat` ou `docker` attestor com labels específicos.

### 4. mTLS como defesa em profundidade
O composite token já autentica o agente na camada de aplicação. O mTLS adiciona autenticação mútua na camada de transporte: o MCP Server recusa conexões de workloads fora do trust domain, mesmo que o token seja válido. Isso mitiga ataques de confused deputy e man-in-the-middle.

### 5. Auth Server como módulo Go independente
O `auth-server/` possui `go.mod` próprio e pode ser extraído para repositório separado sem refatoração. O roadmap de extensão (introspection, revogação, PKCE, device flow) não afeta os outros módulos do monorepo.

### 6. Composite token de curta duração (5 min)
O agente solicita um novo token por sessão de trabalho. A janela de replay é pequena. Se o agente for comprometido, o raio de explosão é limitado ao TTL do token e à audience específica do MCP Server.

---

## Antipadrões evitados

| Antipadrão | Por quê é problemático | O que fazemos |
|---|---|---|
| Repassar JWT do usuário ao MCP Server | Exposição de credenciais além do escopo necessário | O agente faz o exchange; o MCP Server recebe apenas o composite token |
| API keys estáticas por agente | Não rotacionam, não são auditáveis por identidade | SVIDs de curta duração emitidos pelo SPIRE CA |
| MCP Server recebe SVID bruto | O SVID é uma credencial de infra, não de aplicação | MCP Server recebe composite token com contexto de usuário |
| Tokens sem audiência | Reutilizáveis em qualquer serviço | `aud` vincula o token ao MCP Server específico |
| Credenciais de longa duração | Comprometimento silencioso por períodos longos | JWT SVID TTL 1h, composite token TTL 5min |

---

## Referências

- Internet-Draft IETF: [`draft-klrc-aiagent-auth-00`](https://datatracker.ietf.org/doc/draft-klrc-aiagent-auth/)
- RFC 8693: [OAuth 2.0 Token Exchange](https://www.rfc-editor.org/rfc/rfc8693)
- RFC 7523: [JWT Profile for OAuth 2.0 Client Authentication](https://www.rfc-editor.org/rfc/rfc7523)
- RFC 9068: [JWT Profile for OAuth 2.0 Access Tokens](https://www.rfc-editor.org/rfc/rfc9068)
- SPIFFE/SPIRE: [https://spiffe.io/docs/latest/](https://spiffe.io/docs/latest/)
- go-spiffe SDK: [https://github.com/spiffe/go-spiffe](https://github.com/spiffe/go-spiffe)
