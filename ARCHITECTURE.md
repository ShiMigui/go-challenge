# ARCHITECTURE.md — Decisões de Arquitetura

Este documento registra as decisões técnicas tomadas na implementação do desafio de processamento distribuído de apostas.

---

## 1. Representação de Dinheiro (`Money`)

### Escolha: `int64` em unidades mínimas (centavos)

- **Por que não `float32/float64`**: Ponto flutuante introduz erros de arredondamento inaceitáveis em finanças (ex: `0.1 + 0.2 != 0.3`).
- **Por que não `NUMERIC`/`DECIMAL` no banco**: Aritmética em Go e no banco seriam operações diferentes, exigindo conversões e risco de divergência. Com `BIGINT` (unidades mínimas), a soma em Go (`a + b`) é a mesma operação que `a + b` no SQL.
- **Escala fixa**: 2 casas decimais (centavos). Código de moeda ISO 4217 (`CHAR(3)`).
- **Parsing**: Rejeita vazios, `NaN`, `Infinity`, notação científica, escala excedente (>2 casas), negativos em entradas externas.
- **Overflow**: Verificado no parsing, soma, subtração e negação (`math.MaxInt64` / `math.MinInt64`).
- **Moeda**: Carregada no value object. Operações aritméticas e comparação exigem moedas iguais (`ErrCurrencyMismatch`).
- **Testes de incompatibilidade**: Incluídos nos testes unitários (`money_test.go`).

**Limite**: `int64` suporta até ±9.223.372.036.854,77 (92 quatrilhões) — suficiente para volumes reais em centavos.

---

## 2. Transações SQL e Delimitação

### Estratégia: Transação única por operação de domínio

Cada caso de uso (ex: `SubmitTransaction`, `CreateWallet`) executa em **uma única transação `pgx.Tx`** que abrange:

1. Alterações de domínio (`wallets`, `wager_transactions`)
2. Ledger (`wallet_ledger_entries`)
3. Inbox (se entrada SQS)
4. Outbox (eventos de integração)

**Nenhum commit intermediário** — ou tudo confirma ou nada. Isso garante atomicidade financeira e que eventos só saem após o commit (requisito 7.4).

### Repositórios

- Repositórios recebem `pgx.Tx` (ou `Querier` genérico) — não abrem/fecham transações.
- Caso de uso (service) controla `Begin`/`Commit`/`Rollback` via `pgx.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})`.
- `RepeatableRead` + compare-and-set na versão da carteira impede lost updates sem lock de linha longo.

---

## 3. Idempotência Persistente

### Duas chaves, ambas persistidas no banco

| Chave | Tabela | Unique Constraint |
|---|---|---|
| `(provider_id, external_transaction_id)` | `wager_transactions` | Impede reprocessar a mesma operação externa |
| `(provider_id, idempotency_key)` | `wager_transactions` | Impede reuso de chave com payload diferente |

### Hash canônico do payload

- JSON canônico (chaves ordenadas, sem espaços) dos **campos de negócio**.
- Exclui: `idempotency_key`, metadados de transporte (headers, envelope SQS).
- Mesmo payload por HTTP ou SQS → mesmo hash → mesma deduplicação.
- Conflito de hash na mesma `idempotency_key` → `409 IDEMPOTENCY_CONFLICT`.

### Replay

- Mesmo `idempotency_key` + mesmo payload → retorna resultado persistido com `idempotentReplay: true` e **saldo observado no processamento original** (não o saldo atual).
- Persistido em `wager_transactions.observed_balance` no momento do processamento.

### SQS

- `data.idempotencyKey` no payload da mensagem.
- Inbox adiciona dedup por `(consumer_name, message_id)` + hash da mensagem.
- Dupla proteção: aplicação (idempotency_key) + infraestrutura (inbox).

---

## 4. Controle de Concorrência (Carteiras)

### Estratégia: Otimista com versão + Compare-and-Set

- `wallets.version` inicia em `1`, incrementada **apenas** quando `balance` muda.
- Trigger `wallets_bump_version` (`BEFORE UPDATE`) incrementa versão e atualiza `updated_at` automaticamente.
- Escrita no back: `UPDATE wallets SET balance = $novo WHERE id = $id AND version = $lida`
- Se 0 linhas → conflito → retry com leitura nova (máx. 3 tentativas, backoff curto).
- **Lock pessimista (`SELECT FOR UPDATE`) NÃO usado** — permite carteiras independentes avançarem em paralelo (requisito 7.6).

### Teste obrigatório validado

> Carteira com 100.00 BRL recebe duas apostas de 80.00 BRL simultâneas.
> Resultado: uma `PROCESSED`, uma `REJECTED` (INSUFFICIENT_FUNDS), saldo final 20.00, um único débito no ledger.

Executado em `internal/application/wagering/concurrency_test.go` com 3 instâncias paralelas.

---

## 5. Referências Pendentes (`PENDING_REFERENCE`)

### Fluxo

1. `REFUND`/`ROLLBACK` chega antes da referência → persiste como `PENDING_REFERENCE`, `reference_transaction_id = NULL`.
2. Worker de referências (background) faz polling em `wager_transactions` com `state = 'PENDING_REFERENCE'` e `reference_next_attempt_at <= now()`.
3. Resolve `reference_external_transaction_id` → `reference_transaction_id` (FK interna).
4. Se referência `PROCESSED` → aplica reversão.
5. Se referência `REJECTED`/`FAILED` → rejeita a reversão com `REFERENCE_NOT_PROCESSED`.
6. Backoff exponencial: `2s, 4s, 8s, 16s, 32s...` (máx 5 min).
7. Máximo de tentativas: **10** (configurável). Após esgotar → `REJECTED` com `REFERENCE_NOT_FOUND`.

### Persistência da espera

- A inbox da mensagem SQS pode ser concluída (`completed_at` preenchido) assim que a pendência está durável.
- O worker de referências assume a continuidade — não há perda se o processo morrer.

---

## 6. Reversões (`REFUND` / `ROLLBACK`)

### Regras

| Tipo | Movimento | Referência obrigatória? | Valor |
|---|---|---|---|
| `REFUND` | Crédito | Sim (`referenceExternalTransactionId`) | Igual ao da `BET` referenciada |
| `ROLLBACK` | Débito (contrário ao original) | Sim | Igual ao da operação referenciada |

### Validações

- Mesma `provider_id`, `player_id`, `wallet_id`, `currency`, `round_id`.
- Valor **exatamente igual** ao da referência (reversão parcial não permitida).
- Referência deve estar `PROCESSED`.
- Índice único parcial impede dois `REFUND` (ou dois `ROLLBACK`) bem-sucedidos na mesma referência:
  ```sql
  CREATE UNIQUE INDEX wager_tx_single_successful_reversal_per_type_uniq
      ON wager_transactions (reference_transaction_id, kind)
      WHERE reference_transaction_id IS NOT NULL
        AND state = 'PROCESSED'
        AND kind IN ('REFUND', 'ROLLBACK');
  ```
- `ROLLBACK` que precisaria debitar mais que o saldo → `REJECTED` com `INSUFFICIENT_FUNDS_ON_REVERSAL` (código diferente de `INSUFFICIENT_FUNDS` de `BET`).

---

## 7. Inbox / Outbox

### Inbox

- Tabela `inbox`: `(consumer_name, message_id)` unique.
- `message_hash` valida integridade em reentregas.
- Gravada **na mesma transação** do domínio + ledger + outbox.
- Mensagem removida da fila SQS **após commit**.
- `PENDING_REFERENCE`: inbox concluída ao persistir a pendência; worker de referências continua.

### Outbox

- Tabela `outbox`: `id` = `eventId` estável (UUID v7), `payload` = snapshot JSONB imutável.
- Escrita na mesma transação do domínio.
- Worker separado publica (polling em `published_at IS NULL` ordenado por `next_attempt_at`).
- **Disputa por registro**: `locked_by` + `locked_at` (par consistente). Worker que morre deixa lock expirar; outra instância retoma.
- Backoff: exponencial com jitter, máx 5 min.
- Republicação preserva `eventId` → consumidor downstream deduplica.

### Eventos publicados

| Evento | Gatilho | Payload chave |
|---|---|---|
| `WagerTransactionProcessed` | Conclusão bem-sucedida (inclui `LOSS`) | `transactionId`, `kind`, `state`, `balance`, `walletVersion` |
| `WagerTransactionRejected` | Rejeição definitiva por regra de negócio | `transactionId`, `failureCode`, `failureMessage` |
| `WalletBalanceChanged` | Alteração efetiva do saldo | `walletId`, `transactionId`, `direction`, `money`, `balanceBefore`, `balanceAfter`, `walletVersion` |
| `WagerTransactionPendingReference` | Registro de espera pela referência | `transactionId`, `referenceExternalTransactionId`, `referenceNextAttemptAt` |

Envelope: `eventId`, `eventType`, `aggregateId`, `correlationId`, `causationId` (opcional), `occurredAt` (RFC3339 UTC), `version`, `data` tipado.

---

## 8. Autenticação e Autorização

### IdP: Keycloak (Docker Compose)

- Realm `wagering`, clients: `wagering-api` (confidential), `provider-a`, `provider-b`, `wagering-internal`.
- Fluxo: `client_credentials` para serviços; tokens JWT RS256.
- Validação: JWKS do Keycloak, verifica `iss`, `aud`, `exp`, `nbf`, assinatura.

### Modelo de permissões

| Token (claim `provider_id`) | Acesso |
|---|---|
| `provider-a` | Apenas transações onde `provider_id = 'provider-a'` |
| `provider-b` | Apenas transações onde `provider_id = 'provider-b'` |
| `wagering-internal` | Operações de carteira (`POST /wallets`, `GET /wallets/*`, `POST /wallets/*/reconciliation`) |

### Middleware `AuthMiddleware`

1. Extrai `Authorization: Bearer <token>`.
2. Valida JWT contra JWKS.
3. Extrai `provider_id` do claim configurado (`provider_id` ou `azp`).
4. Injeta no `context.Context` para handlers.
5. Handlers de wagering filtram consultas por `provider_id` do token.
6. Handlers de wallet exigem token interno (`wagering-internal`).

### Isolamento

- `GET /wagering/transactions/:id` → filtra por `provider_id` do token.
- `GET /providers/:providerId/wagering/transactions/:extId` → valida que `:providerId` = token.
- Replay respeita isolamento: mesma chave + outro provider → 403/404.

---

## 9. Composição com Uber Fx

### Módulos

| Módulo | Fornece |
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

- `fx.Lifecycle` gerencia:
  - **Start**: Valida config, abre pool DB, cria filas SQS, inicia HTTP server, inicia workers (consumer, reference resolver, outbox publisher).
  - **Stop**: Para aceitar novas requisições/consumo, aguarda trabalho em andamento (timeout 30s), fecha pool DB, fecha conexões SQS.
- `fx.NopLogger` em produção; `zap`/`slog` em desenvolvimento.

---

## 10. Shutdown Gracioso

### HTTP Server

- `http.Server.Shutdown(ctx)` com deadline (`SHUTDOWN_TIMEOUT=30s`).
- Para de aceitar novas conexões; aguarda requests em voo.

### SQS Consumer

- Para `ReceiveMessage` loop.
- Aguarda mensagens em processamento terminarem (ou timeout).
- Se timeout → não deleta da fila → `visibility timeout` expira → reentrega segura.

### Outbox Publisher

- Para de buscar novos registros.
- Conclui publicação do registro atual (ou libera lock).

### Workers de Referência

- Para polling.
- Conclui tentativa atual.

### Ordem de fechamento (Fx)

1. HTTP Server (para entrada nova)
2. SQS Consumer (para entrada nova)
3. Workers (concluem trabalho)
4. Outbox Publisher (conclui publicação)
5. Database Pool (após todos que usam)

---

## 11. Health Checks

| Endpoint | Verifica | Código |
|---|---|---|
| `GET /health/live` | Processo vivo | 200 sempre |
| `GET /health/ready` | DB conectado + SQS acessível | 200 se ambos OK, 503 se algum DOWN |

Usados por Docker Compose (`healthcheck`) e orquestradores.

---

## 12. Observabilidade

### Logs

- `slog` JSON com campos: `level`, `msg`, `time`, `correlationId`, `messageId`, `transactionId`, `walletId`, `providerId`.
- **Não** loga credenciais, tokens, payloads financeiros completos.
- Request ID propagado via header `X-Request-ID` ou gerado.

### Métricas (Prometheus)

| Métrica | Tipo | Labels |
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

## 13. Limitações e Interpretações

| Item | Decisão |
|---|---|
| Moedas | Suporte completo a ISO 4217 no domínio; testes cobrem incompatibilidade. Cenários principais em BRL. |
| `OPENING` por transporte | Rejeitado no handler (HTTP/SQS). Banco não distingue canal — limitação documentada em `docs/schema.md`. |
| `REFUND` + `ROLLBACK` mesma referência | Índice impede mesmo tipo. Tipos diferentes convivem (decisão de produto). |
| `failure_code` em `PENDING_REFERENCE` | Permitido `NULL` para não forçar worker a preencher antes da hora. |
| Partidas dobradas | Não implementado (opcional). Ledger é single-entry com direção. |
| Tracing OpenTelemetry | Não implementado (opcional). Logs com `correlationId` permitem rastreamento. |
| Carga | Sem meta de RPS. Testes de concorrência validam correção, não throughput. |

---

## 14. Trabalho Não Concluído / Melhorias Futuras

- [ ] Partidas dobradas (double-entry ledger) para auditoria contábil completa.
- [ ] OpenTelemetry tracing integrado.
- [ ] Rate limiting por provider no API Gateway / middleware.
- [ ] Dashboard Grafana pré-configurado.
- [ ] Testes de carga automatizados (k6/Gatling) com comando reproduzível.
- [ ] Rotação de chaves JWKS automática (cache com TTL).
- [ ] Métricas de latência por percentil (p50/p95/p99) no health/ready.