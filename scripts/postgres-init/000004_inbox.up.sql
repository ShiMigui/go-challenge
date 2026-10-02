CREATE TABLE inbox (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    consumer_name TEXT NOT NULL,
    message_id TEXT NOT NULL,
    -- Hash do corpo. Divergencia em reentrega indica mensagem adulterada.
    message_hash TEXT NOT NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- NULL enquanto o processamento nao foi confirmado. A mensagem so sai
    -- da fila depois do commit.
    completed_at TIMESTAMPTZ CHECK (completed_at IS NULL OR completed_at >= received_at),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),

    CONSTRAINT inbox_consumer_message_uniq UNIQUE (consumer_name, message_id)
);

COMMENT ON TABLE inbox IS
    'Deduplicacao duravel por consumidor. Gravado na mesma transacao das alteracoes de dominio.';
COMMENT ON COLUMN inbox.message_hash IS
    'Hash do corpo. Divergencia em reentrega indica mensagem adulterada.';

CREATE INDEX inbox_pending_idx
    ON inbox (received_at)
    WHERE completed_at IS NULL;