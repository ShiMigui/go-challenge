CREATE TABLE inbox (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    consumer_name TEXT NOT NULL,
    message_id TEXT NOT NULL,
    -- Hash do corpo. Divergencia em reentrega indica mensagem adulterada.
    message_hash TEXT NOT NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ,
    attempts INTEGER NOT NULL DEFAULT 0,

    CONSTRAINT inbox_consumer_message_uniq UNIQUE (consumer_name, message_id),
    CONSTRAINT inbox_attempts_non_negative CHECK (attempts >= 0),
    -- completed_at nunca pode preceder o recebimento.
    CONSTRAINT inbox_completed_after_received CHECK (
        completed_at IS NULL OR completed_at >= received_at
    )
);

COMMENT ON TABLE inbox IS
    'Deduplicacao duravel por consumidor. Gravado na mesma transacao das alteracoes de dominio.';
COMMENT ON COLUMN inbox.completed_at IS
    'NULL enquanto o processamento nao foi confirmado. Mensagem so e removida da fila apos o commit.';

CREATE INDEX inbox_pending_idx
    ON inbox (received_at)
    WHERE completed_at IS NULL;