CREATE TABLE outbox (
    -- eventId estavel: preservado entre republicacoes.
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    aggregate_type TEXT NOT NULL,
    aggregate_id UUID NOT NULL,
    event_type TEXT NOT NULL,
    event_version INTEGER NOT NULL,

    correlation_id TEXT,
    causation_id UUID,

    -- Snapshot imutavel do envelope publicado. Nao e reescrito no retry.
    payload JSONB NOT NULL,

    occurred_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    attempts INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at TIMESTAMPTZ,

    -- Worker que assumiu o registro, para recuperar trabalho abandonado.
    locked_by TEXT,
    locked_at TIMESTAMPTZ,

    CONSTRAINT outbox_event_version_positive CHECK (event_version >= 1),
    CONSTRAINT outbox_attempts_non_negative CHECK (attempts >= 0),
    CONSTRAINT outbox_payload_is_object CHECK (jsonb_typeof(payload) = 'object'),

    -- So um registro pode estar publicado ou descartado, nunca os dois.
    CONSTRAINT outbox_published_after_occurrence CHECK (
        published_at IS NULL OR published_at >= occurred_at
    ),
    CONSTRAINT outbox_lock_is_consistent CHECK (
        (locked_by IS NULL AND locked_at IS NULL)
        OR (locked_by IS NOT NULL AND locked_at IS NOT NULL)
    )
);

COMMENT ON TABLE outbox IS
    'Transactional outbox. Escrito na mesma transacao do dominio, publicado por worker separado.';
COMMENT ON COLUMN outbox.payload IS
    'Snapshot imutavel do envelope. Republicacoes reaproveitam este payload e o mesmo id.';
COMMENT ON COLUMN outbox.locked_by IS
    'Instancia que assumiu o registro. Worker orfao e recuperado por next_attempt_at expirado.';

-- Fila do publisher: apenas registros pendentes, ordenados por prazo.
CREATE INDEX outbox_pending_idx
    ON outbox (next_attempt_at, occurred_at)
    WHERE published_at IS NULL;

-- Reivindicacao de registro cujo worker sumiu sem concluir.
CREATE INDEX outbox_stale_lock_idx
    ON outbox (locked_at)
    WHERE published_at IS NULL AND locked_at IS NOT NULL;

CREATE INDEX outbox_aggregate_idx
    ON outbox (aggregate_type, aggregate_id);