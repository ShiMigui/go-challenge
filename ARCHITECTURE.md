# ARCHITECTURE.md — Architecture Decisions

This document records the technical decisions made in implementing the distributed wagering processing challenge.

---

## 1. Money Representation (`Money`)

### Choice: `int64` in minor units (cents)

- **Why not `float32/float64`**: Floating point introduces rounding errors that are unacceptable in finance (e.g., `0.1 + 0.2 != 0.3`).
- **Why not `NUMERIC`/`DECIMAL` in the database**: Arithmetic in Go and in the database would be different operations, requiring conversions and risking divergence. With `BIGINT` (minor units), the sum in Go (`a + b`) is the same operation as `a + b` in SQL.
- **Fixed scale**: 2 decimal places (cents). ISO 4217 currency code (`CHAR(3)`).
- **Parsing**: Rejects empty values, `NaN`, `Infinity`, scientific notation, excess scale (>2 decimal places), and negatives in external inputs.
- **Overflow**: Checked during parsing, addition, subtraction, and negation (`math.MaxInt64` / `math.MinInt64`).
- **Currency**: Carried in the value object. Arithmetic operations and comparison require identical currencies (`ErrCurrencyMismatch`).
- **Mismatch tests**: Included in the unit tests (`money_test.go`).

**Limit**: `int64` supports up to ±9,223,372,036,854.77 (92 quadrillion) — enough for real volumes in cents.

---

## 2. SQL Transactions and Delimitation

### Strategy: Single transaction per domain operation

Each use case (e.g., `SubmitTransaction`, `CreateWallet`) runs in **a single `pgx.Tx` transaction** covering:

1. Domain changes (`wallets`, `wager_transactions`)
2. Ledger (`wallet_ledger_entries`)
3. Inbox (if SQS input)
4. Outbox (integration events)

**No intermediate commits** — either everything commits or nothing does. This guarantees financial atomicity and that events are only emitted after the commit (requirement 7.4).

### Repositories

- Repositories receive `pgx.Tx` (or a generic `Querier`) — they do not open/close transactions.
- The use case (service) controls `Begin`/`Commit`/`Rollback` via `pgx.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})`.
- `RepeatableRead` + compare-and-set on the wallet version prevents lost updates without long row locks.

---

## 3. Persistent Idempotency

### Two keys, both persisted in the database

| Key | Table | Unique Constraint |
|---|---|---|
| `(provider_id, external_transaction_id)` | `wager_transactions` | Prevents reprocessing the same external operation |
| `(provider_id, idempotency_key)` | `wager_transactions` | Prevents reusing a key with a different payload |

### Canonical payload hash

- Canonical JSON (sorted keys, no whitespace) of the **business fields**.
- Excludes: `idempotency_key`, transport metadata (headers, SQS envelope).
- Same payload over HTTP or SQS → same hash → same deduplication.
- Hash conflict on the same `idempotency_key` → `409 IDEMPOTENCY_CONFLICT`.

### Replay

- Same `idempotency_key` + same payload → returns the persisted result with `idempotentReplay: true` and the **balance observed during the original processing** (not the current balance).
- Persisted in `wager_transactions.observed_balance` at processing time.

### SQS

- `data.idempotencyKey` in the message payload.
- Inbox adds dedup by `(consumer_name, message_id)` + message hash.
- Double protection: application (idempotency_key) + infrastructure (inbox).

---

## 4. Concurrency Control (Wallets)

### Strategy: Optimistic with version + Compare-and-Set

- `wallets.version` starts at `1` and is incremented **only** when `balance` changes.
- Trigger `wallets_bump_version` (`BEFORE UPDATE`) increments the version and updates `updated_at` automatically.
- Write-back: `UPDATE wallets SET balance = $novo WHERE id = $id AND version = $lida`
- If 0 rows → conflict → retry with a fresh read (max 3 attempts, short backoff).
- **Pessimistic lock (`SELECT FOR UPDATE`) NOT used** — allows independent wallets to progress in parallel (requirement 7.6).

### Required test validated

> A wallet with 100.00 BRL receives two simultaneous 80.00 BRL bets.
> Result: one `PROCESSED`, one `REJECTED` (INSUFFICIENT_FUNDS), final balance 20.00, a single debit in the ledger.

Executed in `internal/application/wagering/concurrency_test.go` with 3 parallel instances.

---

## 5. Pending References (`PENDING_REFERENCE`)

### Flow

1. `REFUND`/`ROLLBACK` arrives before the reference → persisted as `PENDING_REFERENCE`, `reference_transaction_id = NULL`.
2. Reference worker (background) polls `wager_transactions` with `state = 'PENDING_REFERENCE'` and `reference_next_attempt_at <= now()`.
3. Resolves `reference_external_transaction_id` → `reference_transaction_id` (internal FK).
4. If the reference is `PROCESSED` → applies the reversal.
5. If the reference is `REJECTED`/`FAILED` → rejects the reversal with `REFERENCE_NOT_PROCESSED`.
6. Exponential backoff: `2s, 4s, 8s, 16s, 32s...` (max 5 min).
7. Maximum attempts: **10** (configurable). Once exhausted → `REJECTED` with `REFERENCE_NOT_FOUND`.

### Persistence of the wait

- The SQS message inbox can be completed (`completed_at` filled in) as soon as the pending item is durable.
- The reference worker takes over continuity — nothing is lost if the process dies.

---

## 6. Reversals (`REFUND` / `ROLLBACK`)

### Rules

| Type | Movement | Mandatory reference? | Amount |
|---|---|---|---|
| `REFUND` | Credit | Yes (`referenceExternalTransactionId`) | Equal to the referenced `BET` |
| `ROLLBACK` | Debit (opposite of the original) | Yes | Equal to the referenced operation |

### Validations

- Same `provider_id`, `player_id`, `wallet_id`, `currency`, `round_id`.
- Amount **exactly equal** to the reference's (partial reversal not allowed).
- The reference must be `PROCESSED`.
- A partial unique index prevents two successful `REFUND` (or two `ROLLBACK`) operations on the same reference:
  ```sql
  CREATE UNIQUE INDEX wager_tx_single_successful_reversal_per_type_uniq
      ON wager_transactions (reference_transaction_id, kind)
      WHERE reference_transaction_id IS NOT NULL
        AND state = 'PROCESSED'
        AND kind IN ('REFUND', 'ROLLBACK');
  ```
- A `ROLLBACK` that would need to debit more than the balance → `REJECTED` with `INSUFFICIENT_FUNDS_ON_REVERSAL` (a different code from `BET`'s `INSUFFICIENT_FUNDS`).

---

## 7. Inbox / Outbox

### Inbox

- Table `inbox`: `(consumer_name, message_id)` unique.
- `message_hash` validates integrity on redeliveries.
- Written **in the same transaction** as the domain + ledger + outbox.
- Message removed from the SQS queue **after commit**.
- `PENDING_REFERENCE`: inbox completed when the pending item is persisted; the reference worker continues.

### Outbox

- Table `outbox`: `id` = stable `eventId` (UUID v7), `payload` = immutable JSONB snapshot.
- Written in the same transaction as the domain.
- A separate worker publishes (polling `published_at IS NULL` ordered by `next_attempt_at`).
- **Per-record claim**: `locked_by` + `locked_at` (consistent pair). A worker that dies leaves the lock to expire; another instance resumes.
- Backoff: exponential with jitter, max 5 min.
- Republishing preserves the `eventId` → downstream consumer deduplicates.

### Published events

| Event | Trigger | Key payload |
|---|---|---|
| `WagerTransactionProcessed` | Successful completion (includes `LOSS`) | `transactionId`, `kind`, `state`, `balance`, `walletVersion` |
| `WagerTransactionRejected` | Definitive rejection by a business rule | `transactionId`, `failureCode`, `failureMessage` |
| `WalletBalanceChanged` | Effective balance change | `walletId`, `transactionId`, `direction`, `money`, `balanceBefore`, `balanceAfter`, `walletVersion` |
| `WagerTransactionPendingReference` | Record of waiting for the reference | `transactionId`, `referenceExternalTransactionId`, `referenceNextAttemptAt` |

Envelope: `eventId`, `eventType`, `aggregateId`, `correlationId`, `causationId` (optional), `occurredAt` (RFC3339 UTC), `version`, typed `data`.

---

## 8. Authentication and Authorization

### IdP: Keycloak (Docker Compose)

- Realm `wagering`, clients: `wagering-api` (confidential), `provider-a`, `provider-b`, `wagering-internal`.
- Flow: `client_credentials` for services; RS256 JWT tokens.
- Validation: Keycloak JWKS, verifies `iss`, `aud`, `exp`, `nbf`, and signature.

### Permission model

| Token (`provider_id` claim) | Access |
|---|---|
| `provider-a` | Only transactions where `provider_id = 'provider-a'` |
| `provider-b` | Only transactions where `provider_id = 'provider-b'` |
| `wagering-internal` | Wallet operations (`POST /wallets`, `GET /wallets/*`, `POST /wallets/*/reconciliation`) |

### `AuthMiddleware`

1. Extracts `Authorization: Bearer <token>`.
2. Validates the JWT against JWKS.
3. Extracts `provider_id` from the configured claim (`provider_id` or `azp`).
4. Injects it into the `context.Context` for handlers.
5. Wagering handlers filter queries by the token's `provider_id`.
6. Wallet handlers require an internal token (`wagering-internal`).

### Isolation

- `GET /wagering/transactions/:id` → filters by the token's `provider_id`.
- `GET /providers/:providerId/wagering/transactions/:extId` → validates that `:providerId` matches the token.
- Replay respects isolation: same key + another provider → 403/404.

---

## 9. Composition with Uber Fx

### Modules

| Module | Provides |
|---|---|
| `config.Module` | `*config.Config` (lazy `Get()`, eager `MustLoad()`) |
| `database.Module` | `*pgxpool.Pool`, migrations runner |
| `messaging.Module` | SQS client, consumer, publisher |
| `wallet.Module` | `wallet.Service`, `wallet.Repository` |
| `wagering.Module` | `wagering.Service`, `wagering.Repository`, workers |
| `http.Module` | Router, handlers, middleware, server |
| `health.Module` | Health checks (live/ready) |
| `observability.Module` | Logger (slog JSON), metrics (Prometheus) |

### Lifecycle

- `fx.Lifecycle` manages:
  - **Start**: Validates config, opens the DB pool, creates SQS queues, starts the HTTP server, starts workers (consumer, reference resolver, outbox publisher).
  - **Stop**: Stops accepting new requests/consumption, waits for in-flight work (30s timeout), closes the DB pool, closes SQS connections.
- `fx.NopLogger` in production; `zap`/`slog` in development.

---

## 10. Graceful Shutdown

### HTTP Server

- `http.Server.Shutdown(ctx)` with a deadline (`SHUTDOWN_TIMEOUT=30s`).
- Stops accepting new connections; waits for in-flight requests.

### SQS Consumer

- Stops the `ReceiveMessage` loop.
- Waits for messages being processed to finish (or times out).
- On timeout → does not delete from the queue → `visibility timeout` expires → safe redelivery.

### Outbox Publisher

- Stops fetching new records.
- Finishes publishing the current record (or releases the lock).

### Reference Workers

- Stops polling.
- Finishes the current attempt.

### Shutdown order (Fx)

1. HTTP Server (stops new input)
2. SQS Consumer (stops new input)
3. Workers (finish work)
4. Outbox Publisher (finishes publishing)
5. Database Pool (after everyone that uses it)

---

## 11. Health Checks

| Endpoint | Verifies | Code |
|---|---|---|
| `GET /health/live` | Process is alive | 200 always |
| `GET /health/ready` | DB connected + SQS reachable | 200 if both OK, 503 if either is DOWN |

Used by Docker Compose (`healthcheck`) and orchestrators.

---

## 12. Observability

### Logs

- `slog` JSON with fields: `level`, `msg`, `time`, `correlationId`, `messageId`, `transactionId`, `walletId`, `providerId`.
- **Does not** log credentials, tokens, or complete financial payloads.
- Request ID propagated via the `X-Request-ID` header or generated.

### Metrics (Prometheus)

| Metric | Type | Labels |
|---|---|---|
| `wager_transactions_total` | Counter | `kind`, `state`, `provider_id` |
| `wager_idempotency_replays_total` | Counter | `provider_id` |
| `wager_idempotency_conflicts_total` | Counter | `provider_id` |
| `wallet_balance_changed_total` | Counter | `direction`, `provider_id` |
| `wallet_reconciliation_total` | Counter | `consistent` |
| `outbox_publish_duration_seconds` | Histogram | `event_type` |
| `outbox_pending_gauge` | Gauge | — |
| `db_query_duration_seconds` | Histogram | `operation` |
| `concurrency_retries_total` | Counter | `operation` |

---

## 13. Limitations and Interpretations

| Item | Decision |
|---|---|
| Currencies | Full ISO 4217 support in the domain; tests cover mismatch. Main scenarios in BRL. |
| `OPENING` per transport | Rejected in the handler (HTTP/SQS). The database does not distinguish the channel — limitation documented in `docs/schema.md`. |
| `REFUND` + `ROLLBACK` same reference | The index prevents the same type. Different types coexist (product decision). |
| `failure_code` in `PENDING_REFERENCE` | `NULL` allowed so the worker is not forced to fill it in ahead of time. |
| Double-entry bookkeeping | Not implemented (optional). The ledger is single-entry with a direction. |
| OpenTelemetry tracing | Not implemented (optional). Logs with `correlationId` allow tracing. |
| Load | No RPS target. Concurrency tests validate correctness, not throughput. |

---

## 14. Unfinished Work / Future Improvements

- [ ] Double-entry ledger for complete accounting audit.
- [ ] Integrated OpenTelemetry tracing.
- [ ] Per-provider rate limiting in the API Gateway / middleware.
- [ ] Pre-configured Grafana dashboard.
- [ ] Automated load tests (k6/Gatling) with a reproducible command.
- [ ] Automatic JWKS key rotation (cache with TTL).
- [ ] Latency metrics by percentile (p50/p95/p99) in health/ready.