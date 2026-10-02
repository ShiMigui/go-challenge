# Modelo de dados

Cinco tabelas. `wallets` guarda o saldo, `wager_transactions` registra as
operações, `wallet_ledger_entries` guarda o histórico financeiro imutável, e
`inbox`/`outbox` cuidam da entrada e saída de mensagens.

Dinheiro é sempre `BIGINT` em **unidades mínimas** (centavos). Nunca `float`, nem
em `NUMERIC` — a escolha é para que a aritmética em Go e no banco seja a mesma
operação inteira, sem arredondamento em lugar nenhum.

Nenhuma constraint depende do código da aplicação. `make db-test` prova isso
rodando 68 casos que tentam violar cada regra e esperam o banco recusar.

---

## Diagrama

```
players (externo, provido pelo OIDC)
   │
   │ 1:N
   ▼
wallets ──────────────┐
   │  1:N             │ 1:1
   ▼                  ▼
wager_transactions ──► wallet_ledger_entries
   │  ▲   (self-ref: reversões)
   │  │
   └──┴──► inbox   (dedup da entrada, gravada na mesma transação)

        ──► outbox  (publicação após commit, worker separado)
```

---

## wallets

Raiz do agregado financeiro. Uma linha por par `(player_id, currency)`.

| Campo | Tipo | Papel |
| --- | --- | --- |
| `id` | `UUID` PK | Gerado pelo banco (`gen_random_uuid`) |
| `player_id` | `UUID` | Jogador. Vem do token OIDC, não do payload |
| `currency` | `CHAR(3)` | ISO 4217, `^[A-Z]{3}$`. CHECK na coluna |
| `balance` | `BIGINT` | Unidades mínimas. CHECK `>= 0` |
| `version` | `BIGINT` | Inicia em `1`. **Só o trigger escreve** |
| `created_at` / `updated_at` | `TIMESTAMPTZ` | `updated_at` é derivado do saldo |

**Unicidade:** `UNIQUE (player_id, currency)`. O mesmo jogador tem uma carteira
por moeda, não uma carteira só — por isso a unique é no par e não em
`player_id`.

**O CHECK é sintático, a lista oficial é semântica.** `^[A-Z]{3}$` aceita `QQQ`,
`XBT` (retirado da ISO) e `XAU`, que é ouro. O domínio valida contra a lista
oficial da ISO 4217 (`internal/domain/money`) e recusa o que não é moeda
nacional circulante. Trocar o CHECK por um `ENUM` com os 178 códigos fecharia a
porta também no banco, ao custo de uma migration e de um tipo que engessa a
lista quando a ISO publica novidade.

**`version` é controlada pelo banco.** O trigger `wallets_bump_version` roda
`BEFORE UPDATE` e só age se `balance` mudou de valor, incrementando a versão e
ajustando `updated_at`. O back escreve apenas `balance`; se mandar `version`, a
trigger sobrescreve.

O que **continua no back** é o compare-and-set:

```sql
UPDATE wallets SET balance = $novo WHERE id = $id AND version = $lida
```

Se vier 0 linhas, outro escritor mexeu no saldo entre a leitura e a escrita e a
operação é reaplicada. É isso que impede lost update sem segurar lock de linha
durante a transação inteira. Como a chave da condition é a versão, carteiras
independentes avançam em paralelo — não existe lock global.

**Trigger `wallets_bump_version`:** `AFTER`/`BEFORE UPDATE ... FOR EACH ROW`.

---

## wager_transactions

Cada operação de wagering. `kind` é o único discriminador: `OPENING` é a
abertura interna de carteira, qualquer outro valor veio de fora (HTTP ou SQS).
Não existe coluna de origem porque ela seria estado duplicado, sempre igual a
`(kind = 'OPENING')`.

### Identidade

| Campo | Papel |
| --- | --- |
| `id` | PK interna, gerada pelo banco |
| `provider_id` | Provedor externo. `NULL` só em `OPENING` |
| `external_transaction_id` | Id do provedor. `UNIQUE` com `provider_id` |
| `idempotency_key` | Chave de deduplicação. `UNIQUE` com `provider_id` |
| `payload_hash` | Hash do payload canônico |

As duas uniques compostas são o que torna a idempotência **persistente**: a
dedup do SQS FIFO é apenas otimização, quem garante é o banco. Divergência de
`payload_hash` na mesma chave vira conflito de idempotência, não reexecução.

`NULL` nesses campos em `OPENING` impede que uma abertura interna ocupe a chave
de deduplicação de um provider.

### Contexto

| Campo | Papel |
| --- | --- |
| `player_id` | Jogador |
| `wallet_id` | FK → `wallets(id)` |
| `round_id` / `game_id` | Rodada e jogo. `NULL` em `OPENING` |
| `currency` | ISO 4217. Deve coincidir com a da carteira |
| `amount` | Unidades mínimas. Regra por `kind`, ver abaixo |

### Referência (reversões)

| Campo | Papel |
| --- | --- |
| `reference_external_transaction_id` | Id externo da operação referenciada |
| `reference_transaction_id` | FK auto-referente → `wager_transactions(id)` |

Os dois existem porque a referência pode chegar **antes** da operação original.
A FK interna fica `NULL` enquanto a referência não é resolvida.

`reference_external_transaction_id` é **obrigatória** em `REFUND` e `ROLLBACK`,
**opcional** em `WIN` e **proibida** em `BET`, `LOSS` e `OPENING`. Uma `WIN`
pode citar a aposta da mesma rodada, mas não precisa — nem toda rodada chega
com a aposta visível para quem processa o prêmio.

### Estado

| Campo | Papel |
| --- | --- |
| `state` | `PENDING`, `PENDING_REFERENCE`, `PROCESSED`, `REJECTED`, `FAILED` |
| `failure_code` | Código estável. Obrigatório em `REJECTED`/`FAILED` |
| `failure_message` | Detalhe legível. `NULL` quando `PROCESSED` |
| `reference_attempts` | Tentativas do worker de referências |
| `reference_next_attempt_at` | Backoff. `NULL` quando não há espera |
| `processed_at` | Instante de conclusão. Obrigatório em estado terminal |

`PENDING_REFERENCE` é o estado de espera: a pendência já está persistida e
durável, e o worker de referências assume a partir dali.

### Regras por `kind`

| `kind` | Movimento | Condição |
| --- | --- | --- |
| `OPENING` | Crédito | Abertura interna, sem nenhum campo externo |
| `BET` | Débito | Valor positivo e saldo suficiente |
| `WIN` | Crédito | Valor positivo, pode referenciar aposta da rodada |
| `LOSS` | Nenhum | `amount = 0`. Não gera ledger nem muda `version` |
| `REFUND` | Crédito | Devolve a referência, exige `reference_external_transaction_id` |
| `ROLLBACK` | Débito | Desfaz a referência pelo valor original |

`amount` tem um único CHECK cobrindo `LOSS` e os movimentos:
`(kind = 'LOSS' AND amount = 0) OR (kind <> 'LOSS' AND amount > 0)`.

### Constraint que impede devolução duplicada

```sql
CREATE UNIQUE INDEX wager_tx_single_successful_reversal_per_type_uniq
    ON wager_transactions (reference_transaction_id, kind)
    WHERE reference_transaction_id IS NOT NULL
      AND state = 'PROCESSED'
      AND kind IN ('REFUND', 'ROLLBACK');
```

Uma referência não recebe duas reversões **do mesmo tipo**. O índice é parcial:
só linhas `PROCESSED` contam, então um `REFUND` que foi `REJECTED` não ocupa o
lugar e uma tentativa posterior é aceita normalmente. É por isso que a lógica de
retentativa continua funcionando.

Combinação de tipos diferentes sobre a mesma referência (`REFUND` + `ROLLBACK`)
**não** é bloqueada pelo banco — o índice só cobre o mesmo tipo. Essa é uma
decisão de aplicação: se o produto não deve permitir os dois, a verificação vai
no serviço de reversões.

### Índices

| Índice | Serve a |
| --- | --- |
| `wager_tx_wallet_id_idx` | Histórico da carteira, mais recente primeiro |
| `wager_tx_state_pending_idx` | Parcial em `PENDING`/`PENDING_REFERENCE` |
| `wager_tx_reference_retry_idx` | Parcial: worker de referências, por prazo |
| `wager_tx_reference_transaction_id_idx` | Parcial: reversões de uma referência |

Os quatro são parciais ou compostos para não inflar a tabela, que é a que mais
cresce.

---

## wallet_ledger_entries

Histórico financeiro append-only. Uma linha por movimentação real de saldo.
`LOSS` e rejeições **não** geram entrada.

| Campo | Papel |
| --- | --- |
| `id` | PK |
| `wallet_id` | FK → `wallets(id)` |
| `transaction_id` | FK → `wager_transactions(id)`. `UNIQUE` com `wallet_id` |
| `direction` | `DEBIT` ou `CREDIT`. Carrega o sinal |
| `amount` | Unidades mínimas, **sempre positivo** |
| `currency` | ISO 4217 |
| `balance_before` | Saldo antes. `>= 0` |
| `balance_after` | Saldo depois. `>= 0` |
| `created_at` | Instante |

`UNIQUE (wallet_id, transaction_id)` garante no máximo um lançamento por
transação por carteira — é o que impede crédito inicial duplicado.

O CHECK de aritmética valida a invariante do enunciado:

```sql
(direction = 'CREDIT' AND balance_after = balance_before + amount)
OR (direction = 'DEBIT'  AND balance_after = balance_before - amount)
```

Como o saldo nunca é negativo e o `amount` é positivo, a expressão já implica
`balance_after >= 0` — o CHECK da coluna é reforço, não substituto.

### Append-only no próprio banco

`wallet_ledger_entries_immutable()` levanta `restrict_violation` em três
triggers:

| Trigger | Evento | Nível |
| --- | --- | --- |
| `wallet_ledger_entries_no_update` | `BEFORE UPDATE` | `FOR EACH ROW` |
| `wallet_ledger_entries_no_delete` | `BEFORE DELETE` | `FOR EACH ROW` |
| `wallet_ledger_entries_no_truncate` | `BEFORE TRUNCATE` | `FOR EACH STATEMENT` |

O de `TRUNCATE` é separado porque `TRUNCATE` não dispara trigger de linha — sem
ele, o append-only era furável com um único statement. Correção financeira se
faz com lançamento novo, nunca editando o antigo.

---

## inbox

Deduplicação durável de mensagens recebidas.

| Campo | Papel |
| --- | --- |
| `id` | PK |
| `consumer_name` | Consumidor. Um por tipo de handler |
| `message_id` | Id da mensagem. `UNIQUE` com `consumer_name` |
| `message_hash` | Hash do corpo. Divergência = mensagem adulterada |
| `received_at` | Chegada |
| `completed_at` | `NULL` enquanto não confirmado |
| `attempts` | Tentativas de processamento |

`UNIQUE (consumer_name, message_id)` é o que sobrevive a restart: a dedup do SQS
FIFO tem janela de 5 minutos, essa constraint não.

O registro é gravado **na mesma transação SQL** das alterações de domínio, do
ledger e dos eventos. A mensagem só sai da fila depois do commit — se o processo
morrer antes, a reentrega bate no registro e o trabalho é retomado.

Uma referência pendente pode ter a inbox concluída assim que a pendência estiver
persistida; o worker de referências assume a partir dali.

`inbox_pending_idx` é parcial em `completed_at IS NULL`, para não varrer
mensagens já resolvidas.

---

## outbox

Publicação de eventos depois do commit. Escrita na mesma transação do domínio,
enviada por worker separado.

| Campo | Papel |
| --- | --- |
| `id` | PK. É o `eventId` do envelope, estável entre republicações |
| `aggregate_type` / `aggregate_id` | O que originou o evento |
| `event_type` | Ex.: `WalletBalanceChanged` |
| `event_version` | Versão do contrato do evento. CHECK `>= 1` |
| `correlation_id` | Agrupa operações do mesmo fluxo |
| `causation_id` | Evento que causou este |
| `payload` | `JSONB`. Snapshot imutável do envelope. Deve ser objeto |
| `occurred_at` | Quando o fato aconteceu |
| `attempts` | Tentativas de envio |
| `next_attempt_at` | Backoff, com fila de pendentes |
| `published_at` | `NULL` enquanto não publicado |
| `locked_by` / `locked_at` | Worker que assumiu o registro |

`payload` é snapshot: republicação reaproveita o mesmo JSON e o mesmo `id`. O
`eventId` não muda entre tentativas, então o consumidor consegue deduplicar.

`locked_by`/`locked_at` como par consistente (`outbox_lock_is_consistent`)
permite múltiplos publishers disputando registro. Worker que morre deixa
`locked_at` expirado e outra instância retoma pelo `next_attempt_at`.

`CHECK (published_at IS NULL OR published_at >= occurred_at)` impede registro
marcado como publicado antes de o fato ter ocorrido.

### Índices

| Índice | Serve a |
| --- | --- |
| `outbox_pending_idx` | Parcial em `published_at IS NULL`, por prazo |
| `outbox_stale_lock_idx` | Parcial: recuperar trabalho abandonado |
| `outbox_aggregate_idx` | Eventos de um agregado |

---

## Números

| | |
| --- | --- |
| Tabelas | 5 |
| Enums | 3 (`wager_kind`, `wager_state`, `ledger_direction`) |
| Colunas em `wager_transactions` | 22 |
| Checks em `wager_transactions` | 11, dos quais 4 inline na coluna |
| Triggers | 4 (1 de version, 3 de append-only) |
| Casos em `make db-test` | 72 |

---

## Limites conhecidos

**`OPENING` por transporte virou regra da aplicação.** Sem coluna de origem, o
banco conhece o *formato* mas não o *canal*. Ele rejeita `OPENING` com
`provider_id`, `round_id`, `game_id` ou referência preenchidos — mas um `OPENING`
totalmente sem campos externos é indistinguível de uma abertura legítima. A
rejeição de `OPENING` vindo de HTTP ou SQS precisa acontecer no handler, e a
informação não existe mais em lugar nenhum do schema para recuperá-la.

**`REFUND` e `ROLLBACK` sobre a mesma referência.** O índice impede dois do mesmo
tipo. Tipos diferentes convivem, por decisão. Se o produto não permitir, a
checagem vai no serviço de reversões.

**`failure_code` em `PENDING_REFERENCE`.** As constraints de falha foram
mantidas separadas de propósito. Fundi-las numa bicondicional forçaria
`failure_code IS NULL` também em `PENDING_REFERENCE`, que é justamente onde o
worker de referências pode querer registrar a tentativa. Isso mudaria
comportamento, não seria só cosmeticidade.
