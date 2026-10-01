CREATE TYPE wager_origin AS ENUM ('INTERNAL', 'EXTERNAL');
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

    origin wager_origin NOT NULL,

    -- Somente para origem EXTERNAL. NULL impede claimed_at de ser usado
    -- para Francionar a deduplicacao de operacoes internas.
    provider_id TEXT,
    external_transaction_id TEXT,
    idempotency_key TEXT,
    payload_hash TEXT,

    player_id UUID NOT NULL,
    wallet_id UUID NOT NULL REFERENCES wallets (id),
    round_id TEXT,
    game_id TEXT,
    kind wager_kind NOT NULL,
    currency CHAR(3) NOT NULL,

    -- Unidades minimas. NEGATIVOS permitidos internamente (ROLLBACK debita),
    -- por isso a nao-negatividade fica na tabela e na regra do kind.
    amount BIGINT NOT NULL,

    reference_external_transaction_id TEXT,
    reference_transaction_id UUID REFERENCES wager_transactions (id),

    state wager_state NOT NULL DEFAULT 'PENDING',
    failure_code TEXT,
    failure_message TEXT,

    -- Recontagem da referencia pendente (PENDING_REFERENCE).
    reference_attempts INTEGER NOT NULL DEFAULT 0,
    reference_next_attempt_at TIMESTAMPTZ,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    processed_at TIMESTAMPTZ,
    resolved_at TIMESTAMPTZ,

    CONSTRAINT wager_tx_provider_external_uniq
        UNIQUE (provider_id, external_transaction_id),
    CONSTRAINT wager_tx_provider_idempotency_uniq
        UNIQUE (provider_id, idempotency_key),
    CONSTRAINT wager_tx_currency_iso4217 CHECK (currency ~ '^[A-Z]{3}$'),
    CONSTRAINT wager_tx_reference_attempts_non_negative
        CHECK (reference_attempts >= 0),
    CONSTRAINT wager_tx_updated_after_created CHECK (updated_at >= created_at),

    -- Operacao interna e sempre OPENING e nao carrega nenhum campo externo:
    -- provedor, id externo, chave, hash, rodada, jogo e referencia.
    CONSTRAINT wager_tx_internal_has_no_external_fields CHECK (
        origin <> 'INTERNAL'
        OR (provider_id IS NULL
            AND external_transaction_id IS NULL
            AND idempotency_key IS NULL
            AND payload_hash IS NULL
            AND round_id IS NULL
            AND game_id IS NULL
            AND reference_external_transaction_id IS NULL)
    ),

    -- Operacao externa sempre tem provider, id externo e chave de idempotencia.
    CONSTRAINT wager_tx_external_requires_ids CHECK (
        origin <> 'EXTERNAL'
        OR (provider_id IS NOT NULL
            AND external_transaction_id IS NOT NULL
            AND idempotency_key IS NOT NULL
            AND payload_hash IS NOT NULL)
    ),

    -- Somente OPENING vem da origem interna.
    CONSTRAINT wager_tx_internal_only_opening CHECK (
        origin <> 'INTERNAL' OR kind = 'OPENING'
    ),

    -- OPENING e exclusivo da abertura interna: rejeitado quando chega de
    -- HTTP ou SQS.
    CONSTRAINT wager_tx_opening_only_internal CHECK (
        kind <> 'OPENING' OR origin = 'INTERNAL'
    ),

    -- LOSS nao movimenta dinheiro.
    CONSTRAINT wager_tx_loss_zero_amount CHECK (
        kind <> 'LOSS' OR amount = 0
    ),

    -- BET, WIN, REFUND e ROLLBACK exigem valor positivo.
    CONSTRAINT wager_tx_movement_positive CHECK (
        kind = 'LOSS' OR kind = 'OPENING' OR amount > 0
    ),

    -- REFUND e ROLLBACK dependem de referencia externa.
    CONSTRAINT wager_tx_reversal_requires_reference CHECK (
        kind NOT IN ('REFUND', 'ROLLBACK')
        OR reference_external_transaction_id IS NOT NULL
    ),

    -- Demais tipos nao carregam referencia externa.
    CONSTRAINT wager_tx_non_reversal_has_no_reference CHECK (
        kind IN ('REFUND', 'ROLLBACK')
        OR reference_external_transaction_id IS NULL
    ),

    -- PENDING_REFERENCE exige a referencia interna ainda nao resolvida.
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
    'Operacoes de wagering. origin distingue abertura interna de entrada externa.';
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