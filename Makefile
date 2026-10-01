BINARY   ?= app.bin
MAIN     ?= ./cmd/api
GO       ?= go
HOOKS_DIR ?= scripts/githooks

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

# Instala os hooks usando core.hooksPath, recurso nativo do git (>= 2.9).
# Nao ha symlink, mv ou backup: o git passa a ler os hooks direto do diretorio.
.PHONY: setup
setup:
	@command -v $(GO) >/dev/null 2>&1 || { echo "erro: go nao encontrado no PATH"; exit 1; }
	@command -v git >/dev/null 2>&1 || { echo "erro: git nao encontrado no PATH"; exit 1; }
	@test -d $(HOOKS_DIR) || { echo "erro: $(HOOKS_DIR) nao encontrado"; exit 1; }
	@chmod +x $(HOOKS_DIR)/* 2>/dev/null || true
	@git config core.hooksPath $(HOOKS_DIR)
	@echo "core.hooksPath = $(HOOKS_DIR)"
	@echo "pre-commit: gofmt, go vet e go test"
	@echo "commit-msg: conventional commits"
	@echo "pre-push  : go vet e go test -race"
	@echo
	@echo "para desinstalar: make hooks-uninstall"
	@echo "os hooks sao obrigatorios: nao ha bypass"

.PHONY: hooks-uninstall
hooks-uninstall:
	@git config --unset core.hooksPath 2>/dev/null || true
	@echo "core.hooksPath removido"

.PHONY: test
test:
	$(GO) test ./...

.PHONY: test-race
test-race:
	$(GO) test -race ./...

.PHONY: vet
vet:
	$(GO) vet ./...

.PHONY: clean
clean:
	rm -f $(BINARY)