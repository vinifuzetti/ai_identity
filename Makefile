.PHONY: up down logs register validate run-workload gen-idp-keys gen-idp-keys-force test-exchange test-mcp test-e2e clean

COMPOSE := $(shell docker compose version >/dev/null 2>&1 && echo "docker compose" || echo "docker-compose")

up:
	@COMPOSE="$(COMPOSE)" sh scripts/start.sh

down:
	$(COMPOSE) down -v

logs:
	$(COMPOSE) logs -f

register:
	bash scripts/register-entries.sh

validate:
	@echo "Buscando JWT SVID via Workload API..."
	docker exec spire-agent \
	  /opt/spire/bin/spire-agent api fetch jwt \
	    -audience empresa.com \
	    -socketPath /opt/spire/sockets/agent.sock

run-workload:
	$(COMPOSE) run --rm agent-workload

run-agent:
	@echo "==> Gerando JWT do cliente..."
	$(eval CLIENT_JWT=$(shell docker exec auth-server /gen-client-jwt))
	CLIENT_JWT="$(CLIENT_JWT)" $(COMPOSE) run --rm \
	  -e CLIENT_JWT="$(CLIENT_JWT)" \
	  agent-workload

gen-idp-keys:
	bash scripts/gen-idp-keys.sh

gen-idp-keys-force:
	rm -f config/idp-private.pem config/idp-public.pem
	bash scripts/gen-idp-keys.sh

test-exchange:
	bash scripts/test-exchange.sh

test-mcp:
	bash scripts/test-mcp.sh

test-e2e:
	go test -tags e2e -v -timeout 120s ./test/e2e/...

clean:
	$(COMPOSE) down -v --remove-orphans
