CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE wallets (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    player_id UUID NOT NULL,
    currency CHAR(3) NOT NULL,
    -- Unidades minimas (centavos). INTEGER nunca guarda dinheiro.
    balance BIGINT NOT NULL DEFAULT 0,
    version BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT wallets_player_currency_uniq UNIQUE (player_id, currency),
    CONSTRAINT wallets_balance_non_negative CHECK (balance >= 0),
    CONSTRAINT wallets_currency_iso4217 CHECK (currency ~ '^[A-Z]{3}$'),
    CONSTRAINT wallets_version_positive CHECK (version >= 1),
    CONSTRAINT wallets_updated_after_created CHECK (updated_at >= created_at)
);

COMMENT ON COLUMN wallets.balance IS
    'Saldo em unidades minimas da moeda. Nunca negativo: CHECK na tabela.';
COMMENT ON COLUMN wallets.version IS
    'Inicia em 1 e incrementa apenas quando o saldo muda.';

CREATE INDEX wallets_player_id_idx ON wallets (player_id);