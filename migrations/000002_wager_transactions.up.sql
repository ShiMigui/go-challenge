CREATE TYPE wager_kind AS ENUM ('OPENING', 'BET', 'WIN', 'LOSS', 'REFUND', 'ROLLBACK');
CREATE TYPE wager_state AS ENUM (
    'PENDING',
    'PENDING_REFERENCE',
    'PROCESSED',
    'REJECTED',
    'FAILED'
);

CREATE TABLE wager_transactions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- kind e o unico discriminador: OPENING e a abertura interna de carteira,
    -- todo o resto vem de fora (HTTP ou SQS). Nao ha coluna de origem porque
    -- ela seria estado duplicado, sempre igual a (kind = 'OPENING').
    kind wager_kind NOT NULL,

    -- Somente para origem EXTERNAL. NULL impede que uma operacao interna
    -- ocupe a chave de deduplicacao de um provider.
    provider_id TEXT,
    external_transaction_id TEXT,
    idempotency_key TEXT,
    payload_hash TEXT,

    player_id UUID NOT NULL,
    wallet_id UUID NOT NULL REFERENCES wallets (id),
    round_id TEXT,
    game_id TEXT,
    currency CHAR(3) NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),

    -- Unidades minimas. LOSS nao movimenta dinheiro, todo movimento e positivo.
    amount BIGINT NOT NULL CHECK (
        (kind = 'LOSS' AND amount = 0)
        OR (kind <> 'LOSS' AND amount > 0)
    ),

    reference_external_transaction_id TEXT,
    reference_transaction_id UUID REFERENCES wager_transactions (id),

    state wager_state NOT NULL DEFAULT 'PENDING',
    failure_code TEXT,
    failure_message TEXT,

    -- Recontagem e espera do worker de referencias.
    reference_attempts INTEGER NOT NULL DEFAULT 0 CHECK (reference_attempts >= 0),
    reference_next_attempt_at TIMESTAMPTZ,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now() CHECK (updated_at >= created_at),
    processed_at TIMESTAMPTZ,

    CONSTRAINT wager_tx_provider_external_uniq
        UNIQUE (provider_id, external_transaction_id),
    CONSTRAINT wager_tx_provider_idempotency_uniq
        UNIQUE (provider_id, idempotency_key),

    -- Abertura interna nao carrega nenhum campo externo: provedor, id
    -- externo, chave, hash, rodada, jogo e referencia.
    CONSTRAINT wager_tx_opening_has_no_external_fields CHECK (
        kind <> 'OPENING'
        OR (provider_id IS NULL
            AND external_transaction_id IS NULL
            AND idempotency_key IS NULL
            AND payload_hash IS NULL
            AND round_id IS NULL
            AND game_id IS NULL
            AND reference_external_transaction_id IS NULL)
    ),

    -- Todo o resto vem de provider e precisa de identidade externa para
    -- deduplicar. E o que torna a idempotencia persistente.
    CONSTRAINT wager_tx_external_requires_ids CHECK (
        kind = 'OPENING'
        OR (provider_id IS NOT NULL
            AND external_transaction_id IS NOT NULL
            AND idempotency_key IS NOT NULL
            AND payload_hash IS NOT NULL)
    ),

    -- Referencia externa existe exatamente para REFUND e ROLLBACK.
    CONSTRAINT wager_tx_reference_iff_reversal CHECK (
        (kind IN ('REFUND', 'ROLLBACK'))
        = (reference_external_transaction_id IS NOT NULL)
    ),

    -- PENDING_REFERENCE espera a referencia interna.
    CONSTRAINT wager_tx_pending_reference_requires_unresolved CHECK (
        state <> 'PENDING_REFERENCE'
        OR reference_transaction_id IS NULL
    ),

    -- Estado terminal sempre tem instante de conclusao.
    CONSTRAINT wager_tx_terminal_requires_processed_at CHECK (
        state NOT IN ('PROCESSED', 'REJECTED', 'FAILED')
        OR processed_at IS NOT NULL
    ),

    -- Rejeicao e falha permanente sempre tem codigo estavel.
    CONSTRAINT wager_tx_failure_requires_code CHECK (
        state NOT IN ('REJECTED', 'FAILED')
        OR failure_code IS NOT NULL
    ),

    -- Sucesso nao carrega codigo de falha.
    CONSTRAINT wager_tx_processed_has_no_failure CHECK (
        state <> 'PROCESSED'
        OR (failure_code IS NULL AND failure_message IS NULL)
    )
);

COMMENT ON TABLE wager_transactions IS
    'Operacoes de wagering. kind = OPENING marca a abertura interna; qualquer outro valor veio de fora.';
COMMENT ON COLUMN wager_transactions.amount IS
    'Unidades minimas. Negativo e usado no calculo de ROLLBACK; o saldo nunca e negativo.';
COMMENT ON COLUMN wager_transactions.payload_hash IS
    'Hash do payload canonico. Divergencia na mesma chave vira conflito de idempotencia.';
COMMENT ON COLUMN wager_transactions.reference_next_attempt_at IS
    'Backoff exponencial do worker de referencias. NULL quando nao ha espera.';

CREATE INDEX wager_tx_wallet_id_idx
    ON wager_transactions (wallet_id, created_at DESC);
CREATE INDEX wager_tx_provider_external_lookup_idx
    ON wager_transactions (provider_id, external_transaction_id);
CREATE INDEX wager_tx_state_pending_idx
    ON wager_transactions (state)
    WHERE state IN ('PENDING', 'PENDING_REFERENCE');
CREATE INDEX wager_tx_reference_retry_idx
    ON wager_transactions (reference_next_attempt_at)
    WHERE state = 'PENDING_REFERENCE';
CREATE INDEX wager_tx_reference_transaction_id_idx
    ON wager_transactions (reference_transaction_id)
    WHERE reference_transaction_id IS NOT NULL;

-- Impede que a mesma referencia receba duas reversoes bem-sucedidas do
-- mesmo tipo (garantia do enunciado em "Garanta que uma referencia nao
-- receba duas reversoes bem-sucedidas do mesmo tipo").
CREATE UNIQUE INDEX wager_tx_single_successful_reversal_per_type_uniq
    ON wager_transactions (reference_transaction_id, kind)
    WHERE reference_transaction_id IS NOT NULL
      AND state = 'PROCESSED'
      AND kind IN ('REFUND', 'ROLLBACK');
