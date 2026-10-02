# go-challenge — Processamento Distribuído de Apostas

Serviço em Go para processamento financeiro de operações de provedores de jogos (BET, WIN, LOSS, REFUND, ROLLBACK) com garantias de integridade financeira, idempotência persistente, concorrência e recuperação de falhas.

---

## Pré-requisitos

- **Go 1.23+** (versão declarada em `go.mod` e `Dockerfile`)
- **Docker & Docker Compose** (para PostgreSQL, LocalStack/SQS, Keycloak)
- **Make** (opcional, para comandos de conveniência)

---

## Variáveis de Ambiente

Copie `.env.example` para `.env` e ajuste se necessário:

```bash
cp .env.example .env
```

| Variável | Descrição | Padrão |
|---|---|---|
| `APP_ENV` | `production` ou `development`. Em production, erros internos são ocultados. | `production` |
| `API_PORT` | Porta do servidor HTTP | `8080` |
| `DB_HOST` / `DB_PORT` / `DB_NAME` / `DB_USER` / `DB_PASSWORD` | Conexão PostgreSQL | `postgres:5432/wagering` |
| `CLOUD_ENDPOINT_URL_SQS` | Endpoint LocalStack SQS | `http://localstack:4566` |
| `SQS_QUEUE_NAME` | Fila FIFO principal | `wager-transactions.fifo` |
| `SQS_QUEUE_DLQ_NAME` | Dead-letter queue | `wager-transactions-dlq.fifo` |
| `OIDC_ISSUER_URL` | Keycloak realm URL | `http://keycloak:8080/realms/wagering` |
| `OIDC_AUDIENCE` | Audience esperado no JWT | `wagering-api` |

Valores completos em `.env.example`.

---

## Inicialização Rápida

```bash
# 1. Subir toda a stack (PostgreSQL, LocalStack, Keycloak, API, migrations)
docker compose up --build -d

# 2. Aguardar health checks (≈30s na primeira vez)
docker compose logs -f migrate  # migrations aplicadas automaticamente
docker compose logs -f api      # API pronta em :8080

# 3. Verificar se tudo subiu
curl http://localhost:8080/health/live   # {"status":"UP"}
curl http://localhost:8080/health/ready  # {"status":"UP","checks":{"database":"UP","messaging":"UP"}}
```

> **Nota**: O serviço `migrate` roda as migrations (`.up.sql` em `scripts/postgres/migrations/`) antes da API iniciar. `depends_on` com `condition: service_completed_successfully` garante a ordem.

---

## Estrutura do Projeto

```
.
├── cmd/api/                    # Entry point (main.go + Fx bootstrap)
├── internal/
│   ├── application/            # Casos de uso (wallet, wagering, events)
│   ├── bootstrap/              # Fx modules wiring
│   ├── config/                 # Configuração (lazy Get(), eager MustLoad())
│   ├── domain/                 # Entidades, value objects, erros de domínio
│   │   ├── money/              # Money (int64, parsing, aritmética)
│   │   ├── wallet/             # Wallet aggregate
│   │   ├── wager/              # WagerTransaction, state machine
│   │   ├── ledger/             # WalletLedgerEntry
│   │   ├── identifier/         # Validação de UUIDs
│   │   └── event/              # Eventos de domínio
│   ├── infrastructure/
│   │   ├── persistence/        # Repositórios pgx + SQL explícito
│   │   ├── sqs/                # Consumer, publisher, inbox/outbox workers
│   │   └── health/             # Health checks (DB + SQS)
│   └── interfaces/http/        # Handlers, DTOs, middleware, rotas
├── scripts/
│   └── postgres/
│       ├── migrate.sh          # Runner de migrations
│       └── migrations/         # Fonte única: .up.sql + .down.sql
├── test/
│   └── api_test.sh             # Suite de testes de API (≈100 casos)
├── docker-compose.yml
├── Dockerfile
├── go.mod / go.sum
├── ARCHITECTURE.md             # Decisões técnicas detalhadas
├── CHALLENGE.md                # Enunciado original
└── README.md                   # Este arquivo
```

---

## Migrations

**Fonte única**: `scripts/postgres/migrations/` (6 pares `.up.sql` + `.down.sql`).

### Aplicar (automático no `docker compose up`)

```bash
# Via script standalone (útil em CI/CD)
./scripts/postgres/migrate.sh up

# Ou via Docker (já configurado no compose)
docker compose run --rm migrate
```

### Reverter

```bash
./scripts/postgres/migrate.sh down  # reverte a última
./scripts/postgres/migrate.sh down 3  # reverte 3
./scripts/postgres/migrate.sh down all  # reverte todas
```

### Criar nova migration

```bash
# Cria par de arquivos com timestamp
./scripts/postgres/migrate.sh create "nome_da_migration"
```

---

## Executando a Aplicação

### Via Docker Compose (recomendado)

```bash
docker compose up --build -d
```

### Local (sem containers, requer PostgreSQL/SQS/Keycloak rodando)

```bash
# Configurar .env com endpoints locais
go run ./cmd/api
```

---

## Testes

### Unitários + Integração (PostgreSQL real via testcontainers ou container local)

```bash
go test ./...
go test -race ./...     # Sem data races
go vet ./...            # Sem warnings
```

### Testes de API (HTTP real contra stack completa)

```bash
# Stack deve estar rodando (docker compose up -d)
./test/api_test.sh
```

Saída esperada (exemplo):
```
========================================
  WAGERING API TEST SUITE
========================================
Waiting for API...
API is ready!

=== TEST: Health Live ===
PASS | GET /health/live | Expected: 200 | Got: 200 | Live check: {"status":"UP"}

=== TEST: Create Wallet (Valid) ===
PASS | POST /wallets | Expected: 201 | Got: 201 | Wallet created with ID: ...

...

========================================
  TEST SUMMARY
========================================
Passed: 100+
Failed: 0
```

O script testa: health, wallets (criação, duplicata, moeda inválida, playerId ausente), ledger paginado, reconciliação, BET/WIN/LOSS/REFUND/ROLLBACK, idempotência (replay, conflito), referências (antes/depois, não encontrada), concorrência (duas BETs 80.00 em 100.00), isolamento entre providers, valores inválidos (zero, negativo, moeda errada, kind inválido, referência faltando), e mais.

---

## Exemplos de Chamadas HTTP

### Autenticação

Tokens JWT via Keycloak (`client_credentials`). Exemplos com `curl` assumem token já obtido:

```bash
# Provider A
TOKEN_A=$(curl -s -X POST http://localhost:8080/realms/wagering/protocol/openid-connect/token \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "grant_type=client_credentials&client_id=provider-a&client_secret=provider-a-secret" | jq -r .access_token)

# Provider B
TOKEN_B=$(...)

# Internal (operações de carteira)
TOKEN_INT=$(...)
```

### Criar Carteira (apenas token interno)

```bash
curl -X POST http://localhost:8080/wallets \
  -H "Authorization: Bearer $TOKEN_INT" \
  -H "Content-Type: application/json" \
  -d '{"playerId":"11111111-1111-4111-8111-111111111111","initialBalance":{"amount":"100.00","currency":"BRL"}}'
# 201: {"id":"...","playerId":"...","balance":{"amount":"100.00","currency":"BRL"},"version":1}
```

### Consultar Carteira

```bash
curl http://localhost:8080/wallets/<WALLET_ID> \
  -H "Authorization: Bearer $TOKEN_INT"
# 200: {"id":"...","playerId":"...","balance":{"amount":"100.00","currency":"BRL"},"version":1}
```

### Ledger Paginado

```bash
curl "http://localhost:8080/wallets/<WALLET_ID>/ledger?limit=10" \
  -H "Authorization: Bearer $TOKEN_INT"
# 200: {"entries":[...],"nextCursor":"...","limit":10}
```

### Reconciliação

```bash
curl -X POST http://localhost:8080/wallets/<WALLET_ID>/reconciliation \
  -H "Authorization: Bearer $TOKEN_INT"
# 200: {"walletId":"...","storedBalance":{"amount":"...","currency":"BRL"},"calculatedBalance":{...},"difference":{"amount":"0.00","currency":"BRL"},"consistent":true,"checkedEntries":N}
```

### Enviar Transação (Provider A)

```bash
curl -X POST http://localhost:8080/wagering/transactions \
  -H "Authorization: Bearer $TOKEN_A" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: provider-a:tx-001" \
  -d '{
    "providerId":"provider-a",
    "externalTransactionId":"tx-001",
    "playerId":"11111111-1111-4111-8111-111111111111",
    "walletId":"<WALLET_ID>",
    "roundId":"round-1",
    "gameId":"game-1",
    "kind":"BET",
    "money":{"amount":"25.00","currency":"BRL"}
  }'
# 200: {"transactionId":"...","status":"PROCESSED","balance":{"amount":"75.00","currency":"BRL"},"idempotentReplay":false}
```

### Replay Idempotente (mesma chave)

```bash
# Mesmo comando acima → 200 com idempotentReplay: true e balance = saldo OBSERVADO no processamento original
```

### Conflito de Idempotência (mesma chave, payload diferente)

```bash
# Mesmo Idempotency-Key, externalTransactionId diferente → 409 IDEMPOTENCY_CONFLICT
```

### WIN (crédito)

```bash
curl -X POST http://localhost:8080/wagering/transactions \
  -H "Authorization: Bearer $TOKEN_A" -H "Content-Type: application/json" \
  -H "Idempotency-Key: provider-a:tx-win-1" \
  -d '{"providerId":"provider-a","externalTransactionId":"tx-win-1","playerId":"...","walletId":"<WALLET_ID>","roundId":"round-1","gameId":"game-1","kind":"WIN","money":{"amount":"50.00","currency":"BRL"}}'
```

### LOSS (sem movimentação, amount=0)

```bash
curl -X POST ... -H "Idempotency-Key: provider-a:tx-loss-1" \
  -d '{"...","kind":"LOSS","money":{"amount":"0.00","currency":"BRL"}}'
# 200: status PROCESSED, balance inalterado, sem ledger
```

### REFUND (crédito, requer referência)

```bash
curl -X POST ... -H "Idempotency-Key: provider-a:tx-refund-1" \
  -d '{"...","kind":"REFUND","money":{"amount":"25.00","currency":"BRL"},"referenceExternalTransactionId":"tx-001"}'
# Devolve valor da BET referenciada (tx-001)
```

### ROLLBACK (débito contrário, requer referência)

```bash
curl -X POST ... -H "Idempotency-Key: provider-a:tx-rb-1" \
  -d '{"...","kind":"ROLLBACK","money":{"amount":"50.00","currency":"BRL"},"referenceExternalTransactionId":"tx-win-1"}'
# Desfaz WIN referenciado
```

### Consultar Transação por ID Interno

```bash
curl http://localhost:8080/wagering/transactions/<TX_ID> \
  -H "Authorization: Bearer $TOKEN_A"
```

### Consultar Transação por ID Externo (provider-scoped)

```bash
curl http://localhost:8080/providers/provider-a/wagering/transactions/tx-001 \
  -H "Authorization: Bearer $TOKEN_A"
# Provider B tentando acessar → 403/404
```

---

## Fluxos Autenticados (Keycloak)

O Docker Compose provisiona o Keycloak automaticamente:

- Realm: `wagering`
- Clients: `wagering-api` (confidential), `provider-a`, `provider-b`, `wagering-internal`
- Usuários de teste: `admin`/`admin` (admin console)

### Obter token (provider-a)

```bash
curl -X POST http://localhost:8080/realms/wagering/protocol/openid-connect/token \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "grant_type=client_credentials&client_id=provider-a&client_secret=provider-a-secret"
```

### Isolamento entre providers

- Token `provider-a` só vê/cria transações com `providerId=provider-a`.
- `GET /providers/provider-b/...` com token `provider-a` → 403.
- Replay com token errado → 403/404.

---

## Comandos de Verificação Obrigatórios

```bash
# Formatação
gofmt -l .  # deve retornar vazio

# Testes
go test ./...
go test -race ./...
go vet ./...

# Build
docker compose build
```

---

## Arquitetura Resumida

Veja **`ARCHITECTURE.md`** para decisões detalhadas sobre:

- Dinheiro (`int64` unidades mínimas, sem float)
- Transações SQL (uma por operação, RepeatableRead + CAS)
- Idempotência (duas uniques + hash canônico cross-transporte)
- Concorrência (versão + compare-and-set, sem lock global)
- Referências pendentes (worker com backoff exponencial)
- Reversões (índice único parcial impede duplicata por tipo)
- Inbox/Outbox (mesma transação, workers separados, dispute por lock)
- Autenticação (Keycloak, JWT, isolamento por provider_id)
- Fx composition (modules, lifecycle, shutdown gracioso)
- Observabilidade (logs JSON estruturados, métricas Prometheus)

---

## Limitações Conhecidas

- `OPENING` rejeitado no handler (HTTP/SQS); banco não distingue canal.
- `REFUND` + `ROLLBACK` sobre mesma referência: mesmo tipo bloqueado, tipos diferentes permitidos.
- Partidas dobradas não implementadas (opcional).
- OpenTelemetry tracing não implementado (opcional).
- Rate limiting não implementado.

---

## Licença

Código de desafio técnico. Uso livre para fins de avaliação.