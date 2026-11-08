# go-challenge — Distributed Wagering Processing

Go service for financial processing of game provider operations (BET, WIN, LOSS, REFUND, ROLLBACK) with financial integrity guarantees, persistent idempotency, concurrency, and failure recovery.

---

## Prerequisites

- **Go 1.27.1+** (version declared in `go.mod`, `Dockerfile`, and `docker-compose.yml`)
- **Docker & Docker Compose** (for PostgreSQL, LocalStack/SQS, Keycloak)
- **Make** (optional, for convenience commands)

---

## Environment Variables

Copy `.env.example` to `.env` and adjust if necessary:

```bash
cp .env.example .env
```

| Variable | Description | Default |
|---|---|---|
| `APP_ENV` | `production` or `development`. In production, internal errors are hidden. | `production` |
| `API_PORT` | HTTP server port | `8080` |
| `DB_HOST` / `DB_PORT` / `DB_NAME` / `DB_USER` / `DB_PASSWORD` | PostgreSQL connection | `postgres:5432/wagering` |
| `CLOUD_ENDPOINT_URL_SQS` | LocalStack SQS endpoint | `http://localstack:4566` |
| `SQS_QUEUE_NAME` | Main FIFO queue | `wager-transactions.fifo` |
| `SQS_QUEUE_DLQ_NAME` | Dead-letter queue | `wager-transactions-dlq.fifo` |
| `OIDC_ISSUER_URL` | Keycloak realm URL | `http://keycloak:8080/realms/wagering` |
| `OIDC_AUDIENCE` | Expected audience in the JWT | `wagering-api` |

Full values in `.env.example`.

---

## Quick Start

```bash
# 1. Bring up the whole stack (PostgreSQL, LocalStack, Keycloak, API, migrations)
docker compose up --build -d

# 2. Wait for health checks (≈30s the first time)
docker compose logs -f migrate  # migrations applied automatically
docker compose logs -f api      # API ready on :8080

# 3. Verify everything is up
curl http://localhost:8080/health/live   # {"status":"UP"}
curl http://localhost:8080/health/ready  # {"status":"UP","checks":{"database":"UP","messaging":"UP"}}
```

> **Note**: The `migrate` service runs the migrations (`.up.sql` in `scripts/postgres/migrations/`) before the API starts. `depends_on` with `condition: service_completed_successfully` guarantees the order.

---

## Project Structure

```
.
├── cmd/api/                    # Entry point (main.go + Fx bootstrap)
├── internal/
│   ├── application/            # Use cases (wallet, wagering, events)
│   ├── bootstrap/              # Fx module wiring
│   ├── config/                 # Configuration (lazy Get(), eager MustLoad())
│   ├── domain/                 # Entities, value objects, domain errors
│   │   ├── money/              # Money (int64, parsing, arithmetic)
│   │   ├── wallet/             # Wallet aggregate
│   │   ├── wager/              # WagerTransaction, state machine
│   │   ├── ledger/             # WalletLedgerEntry
│   │   ├── identifier/         # UUID validation
│   │   └── event/              # Domain events
│   ├── infrastructure/
│   │   ├── persistence/        # pgx repositories + explicit SQL
│   │   ├── sqs/                # Consumer, publisher, inbox/outbox workers
│   │   └── health/             # Health checks (DB + SQS)
│   └── interfaces/http/        # Handlers, DTOs, middleware, routes
├── scripts/
│   └── postgres/
│       ├── migrate.sh          # Migration runner
│       └── migrations/         # Single source: .up.sql + .down.sql
├── test/
│   └── api_test.sh             # API test suite (≈100 cases)
├── docker-compose.yml
├── Dockerfile
├── go.mod / go.sum
├── ARCHITECTURE.md             # Detailed technical decisions
├── CHALLENGE.md                # Original challenge statement
└── README.md                   # This file
```

---

## Migrations

**Single source**: `scripts/postgres/migrations/` (6 `.up.sql` + `.down.sql` pairs).

### Apply (automatic on `docker compose up`)

```bash
# Via standalone script (useful in CI/CD)
./scripts/postgres/migrate.sh up

# Or via Docker (already configured in the compose file)
docker compose run --rm migrate
```

### Revert

```bash
./scripts/postgres/migrate.sh down  # reverts the last one
./scripts/postgres/migrate.sh down 3  # reverts 3
./scripts/postgres/migrate.sh down all  # reverts all
```

### Create a new migration

```bash
# Creates a pair of files with a timestamp
./scripts/postgres/migrate.sh create "migration_name"
```

---

## Running the Application

### Via Docker Compose (recommended)

```bash
docker compose up --build -d
```

### Local (without containers, requires PostgreSQL/SQS/Keycloak running)

```bash
# Configure .env with local endpoints
go run ./cmd/api
```

---

## Tests

### Unit + Integration (real PostgreSQL via testcontainers or a local container)

```bash
go test ./...
go test -race ./...     # No data races
go vet ./...            # No warnings
```

### API tests (real HTTP against the full stack)

```bash
# The stack must be running (docker compose up -d)
./test/api_test.sh
```

Expected output (example):
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

The script tests: health, wallets (creation, duplicate, invalid currency, missing playerId), paginated ledger, reconciliation, BET/WIN/LOSS/REFUND/ROLLBACK, idempotency (replay, conflict), references (before/after, not found), concurrency (two 80.00 BETs against 100.00), isolation between providers, invalid values (zero, negative, wrong currency, invalid kind, missing reference), and more.

---

## HTTP Call Examples

### Authentication

JWT tokens via Keycloak (`client_credentials`). The `curl` examples assume a token has already been obtained:

```bash
# Provider A
TOKEN_A=$(curl -s -X POST http://localhost:8080/realms/wagering/protocol/openid-connect/token \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "grant_type=client_credentials&client_id=provider-a&client_secret=provider-a-secret" | jq -r .access_token)

# Provider B
TOKEN_B=$(...)

# Internal (wallet operations)
TOKEN_INT=$(...)
```

### Create Wallet (internal token only)

```bash
curl -X POST http://localhost:8080/wallets \
  -H "Authorization: Bearer $TOKEN_INT" \
  -H "Content-Type: application/json" \
  -d '{"playerId":"11111111-1111-4111-8111-111111111111","initialBalance":{"amount":"100.00","currency":"BRL"}}'
# 201: {"id":"...","playerId":"...","balance":{"amount":"100.00","currency":"BRL"},"version":1}
```

### Query Wallet

```bash
curl http://localhost:8080/wallets/<WALLET_ID> \
  -H "Authorization: Bearer $TOKEN_INT"
# 200: {"id":"...","playerId":"...","balance":{"amount":"100.00","currency":"BRL"},"version":1}
```

### Paginated Ledger

```bash
curl "http://localhost:8080/wallets/<WALLET_ID>/ledger?limit=10" \
  -H "Authorization: Bearer $TOKEN_INT"
# 200: {"entries":[...],"nextCursor":"...","limit":10}
```

### Reconciliation

```bash
curl -X POST http://localhost:8080/wallets/<WALLET_ID>/reconciliation \
  -H "Authorization: Bearer $TOKEN_INT"
# 200: {"walletId":"...","storedBalance":{"amount":"...","currency":"BRL"},"calculatedBalance":{...},"difference":{"amount":"0.00","currency":"BRL"},"consistent":true,"checkedEntries":N}
```

### Submit Transaction (Provider A)

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

### Idempotent Replay (same key)

```bash
# Same command as above → 200 with idempotentReplay: true and balance = balance OBSERVED during the original processing
```

### Idempotency Conflict (same key, different payload)

```bash
# Same Idempotency-Key, different externalTransactionId → 409 IDEMPOTENCY_CONFLICT
```

### WIN (credit)

```bash
curl -X POST http://localhost:8080/wagering/transactions \
  -H "Authorization: Bearer $TOKEN_A" -H "Content-Type: application/json" \
  -H "Idempotency-Key: provider-a:tx-win-1" \
  -d '{"providerId":"provider-a","externalTransactionId":"tx-win-1","playerId":"...","walletId":"<WALLET_ID>","roundId":"round-1","gameId":"game-1","kind":"WIN","money":{"amount":"50.00","currency":"BRL"}}'
```

### LOSS (no movement, amount=0)

```bash
curl -X POST ... -H "Idempotency-Key: provider-a:tx-loss-1" \
  -d '{"...","kind":"LOSS","money":{"amount":"0.00","currency":"BRL"}}'
# 200: status PROCESSED, balance unchanged, no ledger entry
```

### REFUND (credit, requires a reference)

```bash
curl -X POST ... -H "Idempotency-Key: provider-a:tx-refund-1" \
  -d '{"...","kind":"REFUND","money":{"amount":"25.00","currency":"BRL"},"referenceExternalTransactionId":"tx-001"}'
# Returns the amount of the referenced BET (tx-001)
```

### ROLLBACK (opposite debit, requires a reference)

```bash
curl -X POST ... -H "Idempotency-Key: provider-a:tx-rb-1" \
  -d '{"...","kind":"ROLLBACK","money":{"amount":"50.00","currency":"BRL"},"referenceExternalTransactionId":"tx-win-1"}'
# Undoes the referenced WIN
```

### Query Transaction by Internal ID

```bash
curl http://localhost:8080/wagering/transactions/<TX_ID> \
  -H "Authorization: Bearer $TOKEN_A"
```

### Query Transaction by External ID (provider-scoped)

```bash
curl http://localhost:8080/providers/provider-a/wagering/transactions/tx-001 \
  -H "Authorization: Bearer $TOKEN_A"
# Provider B attempting to access → 403/404
```

---

## Authenticated Flows (Keycloak)

Docker Compose provisions Keycloak automatically:

- Realm: `wagering`
- Clients: `wagering-api` (confidential), `provider-a`, `provider-b`, `wagering-internal`
- Test users: `admin`/`admin` (admin console)

### Obtain token (provider-a)

```bash
curl -X POST http://localhost:8080/realms/wagering/protocol/openid-connect/token \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "grant_type=client_credentials&client_id=provider-a&client_secret=provider-a-secret"
```

### Isolation between providers

- The `provider-a` token can only see/create transactions with `providerId=provider-a`.
- `GET /providers/provider-b/...` with a `provider-a` token → 403.
- Replay with the wrong token → 403/404.

---

## Required Verification Commands

```bash
# Formatting
gofmt -l .  # must return empty

# Tests
go test ./...
go test -race ./...
go vet ./...

# Build
docker compose build
```

---

## Architecture Summary

See **`ARCHITECTURE.md`** for detailed decisions on:

- Money (`int64` minor units, no float)
- SQL transactions (one per operation, RepeatableRead + CAS)
- Idempotency (two uniques + canonical cross-transport hash)
- Concurrency (version + compare-and-set, no global lock)
- Pending references (worker with exponential backoff)
- Reversals (partial unique index prevents duplicates per type)
- Inbox/Outbox (same transaction, separate workers, lock-based dispute)
- Authentication (Keycloak, JWT, isolation by provider_id)
- Fx composition (modules, lifecycle, graceful shutdown)
- Observability (structured JSON logs, Prometheus metrics)

---

## Known Limitations

- `OPENING` rejected in the handler (HTTP/SQS); the database does not distinguish the channel.
- `REFUND` + `ROLLBACK` on the same reference: same type blocked, different types allowed.
- Double-entry bookkeeping not implemented (optional).
- OpenTelemetry tracing not implemented (optional).
- Rate limiting not implemented.

---

## License

Technical challenge code. Free to use for evaluation purposes.