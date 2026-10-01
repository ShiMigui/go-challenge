BINARY    ?= app.bin
MAIN      ?= ./cmd/api
GO        ?= go
HOOKS_DIR ?= scripts/githooks
ENV_FILE  ?= .env
ENV_EXAMPLE ?= .env.example

.PHONY: build
build:
	$(GO) build -o $(BINARY) $(MAIN)

.PHONY: fmt
fmt:
	gofmt -w .

.PHONY: fmt-check
fmt-check:
	@files=$$(gofmt -l $$(git ls-files '*.go' 2>/dev/null)); \
	if [ -n "$$files" ]; then \
		echo "arquivos nao formatados (rode 'make fmt'):"; echo "$$files"; exit 1; \
	fi

# Prepara o ambiente local:
#   1. cria o .env a partir do .env.example, se ainda nao existir
#   2. instala os hooks via core.hooksPath, recurso nativo do git (>= 2.9)
#
# O .env nunca e sobrescrito: ajustes locais de credenciais sao preservados.
.PHONY: setup
setup:
	@command -v $(GO) >/dev/null 2>&1 || { echo "erro: go nao encontrado no PATH"; exit 1; }
	@command -v git >/dev/null 2>&1 || { echo "erro: git nao encontrado no PATH"; exit 1; }
	@test -f $(ENV_EXAMPLE) || { echo "erro: $(ENV_EXAMPLE) nao encontrado"; exit 1; }
	@test -d $(HOOKS_DIR) || { echo "erro: $(HOOKS_DIR) nao encontrado"; exit 1; }
	@if [ -f $(ENV_FILE) ]; then \
		echo "$(ENV_FILE) ja existe, mantido como esta"; \
	else \
		cp $(ENV_EXAMPLE) $(ENV_FILE); \
		echo "$(ENV_FILE) criado a partir de $(ENV_EXAMPLE)"; \
	fi
	@chmod +x $(HOOKS_DIR)/* 2>/dev/null || true
	@git config core.hooksPath $(HOOKS_DIR)
	@echo "core.hooksPath = $(HOOKS_DIR)"
	@echo "pre-commit: gofmt, go vet e go test"
	@echo "commit-msg: conventional commits"
	@echo "pre-push  : go vet e go test -race"

.PHONY: test
test:
	$(GO) test ./...

.PHONY: test-race
test-race:
	$(GO) test -race ./...

.PHONY: vet
vet:
	$(GO) vet ./...

.PHONY: db-test
db-test:
	@command -v docker >/dev/null 2>&1 || { echo "erro: docker nao encontrado"; exit 1; }
	@chmod +x migrations/testdata/constraints_test.sh
	@export PGPASSWORD=$${DB_PASSWORD:-wagering}; \
	docker compose exec -T -e PGPASSWORD=$$PGPASSWORD postgres psql -U $${DB_USER:-wagering} -d postgres -q -c "DROP DATABASE IF EXISTS migration_test;" -c "CREATE DATABASE migration_test;" >/dev/null 2>&1; \
	for f in migrations/*.up.sql; do \
		docker compose exec -T -e PGPASSWORD=$$PGPASSWORD postgres psql -U $${DB_USER:-wagering} -d migration_test -v ON_ERROR_STOP=1 -q -f /dev/stdin < "$$f" >/dev/null 2>&1 || { echo "falha aplicando $$f"; exit 1; }; \
	done; \
	migrations/testdata/constraints_test.sh; \
	EXIT=$$?; \
	for f in $$(ls migrations/*.down.sql | sort -r); do \
		docker compose exec -T -e PGPASSWORD=$$PGPASSWORD postgres psql -U $${DB_USER:-wagering} -d migration_test -v ON_ERROR_STOP=1 -q -f /dev/stdin < "$$f" >/dev/null 2>&1 || true; \
	done; \
	docker compose exec -T -e PGPASSWORD=$$PGPASSWORD postgres psql -U $${DB_USER:-wagering} -d postgres -q -c "DROP DATABASE IF EXISTS migration_test;" >/dev/null 2>&1; \
	exit $$EXIT

.PHONY: clean
clean:
	rm -f $(BINARY)
