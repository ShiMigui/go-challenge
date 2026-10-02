CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE wallets(
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    player_id UUID NOT NULL,
    currency CHAR(3) NOT NULL CHECK(currency ~'^[A-Z]{3}$'),
    -- Unidades minimas (centavos). INTEGER nunca guarda dinheiro.
    balance BIGINT NOT NULL DEFAULT 0 CHECK(balance >= 0),
    -- Escrita somente pelo trigger. O back nunca envia este valor.
    version BIGINT NOT NULL DEFAULT 1 CHECK(version >= 1),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now() CHECK(updated_at >= created_at),
    -- Um par (player, currency) por carteira. O mesmo jogador pode ter
    -- carteiras em moedas diferentes.
    CONSTRAINT wallets_player_currency_uniq UNIQUE(player_id, currency)
);

COMMENT ON COLUMN wallets.balance IS 'Saldo em unidades minimas da moeda. Nunca negativo: CHECK na coluna.';
COMMENT ON COLUMN wallets.version IS 'Inicia em 1 e so muda por causa do saldo, via trigger.';

CREATE INDEX wallets_player_id_idx
ON wallets(player_id);

-- "Todo balanco atualizado incrementa a versao" mora no banco. O back
-- escreve apenas o balance; a versao e o updated_at sao derivados aqui.
-- Como o UPDATE condicional (WHERE version = $lida) continua no back, o
-- controle de concurrency otimista permanece intacto.
CREATE FUNCTION wallets_bump_version()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.balance IS DISTINCT FROM OLD.balance THEN
        NEW.version := OLD.version + 1;
        NEW.updated_at := now();
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER wallets_bump_version_on_balance_change
    BEFORE UPDATE ON wallets
    FOR EACH ROW EXECUTE FUNCTION wallets_bump_version();
