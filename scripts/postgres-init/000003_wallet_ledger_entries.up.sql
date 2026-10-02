CREATE TYPE ledger_direction AS ENUM ('DEBIT', 'CREDIT');

CREATE TABLE wallet_ledger_entries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    wallet_id UUID NOT NULL REFERENCES wallets (id),
    transaction_id UUID NOT NULL REFERENCES wager_transactions (id),
    direction ledger_direction NOT NULL,
    -- Unidades minimas, sempre positivo: a direcao carrega o sinal.
    amount BIGINT NOT NULL CHECK (amount > 0),
    currency CHAR(3) NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    balance_before BIGINT NOT NULL CHECK (balance_before >= 0),
    balance_after BIGINT NOT NULL CHECK (balance_after >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT wallet_ledger_wallet_transaction_uniq
        UNIQUE (wallet_id, transaction_id),

    -- O enunciado exige validar balanceAfter = balanceBefore +- money.
    CONSTRAINT wallet_ledger_balance_arithmetic CHECK (
        (direction = 'CREDIT' AND balance_after = balance_before + amount)
        OR
        (direction = 'DEBIT' AND balance_after = balance_before - amount)
    )
);

COMMENT ON TABLE wallet_ledger_entries IS
    'Ledger append-only. Uma mudanca financeira por transacao, confirmada com o saldo.';
COMMENT ON COLUMN wallet_ledger_entries.amount IS
    'Unidades minimas, sempre positivo. O sinal vem de direction.';

CREATE INDEX wallet_ledger_wallet_created_idx
    ON wallet_ledger_entries (wallet_id, created_at DESC, id DESC);

-- Append-only: bloqueia UPDATE e DELETE no proprio banco, sem depender do
-- codigo da aplicacao. UPDATE e DELETE sao recusados mesmo dentro de transacao.
CREATE OR REPLACE FUNCTION wallet_ledger_entries_immutable()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION
        'wallet_ledger_entries e append-only: % nao permitido', TG_OP
        USING ERRCODE = 'restrict_violation';
END;
$$;

CREATE TRIGGER wallet_ledger_entries_no_update
    BEFORE UPDATE ON wallet_ledger_entries
    FOR EACH ROW EXECUTE FUNCTION wallet_ledger_entries_immutable();

CREATE TRIGGER wallet_ledger_entries_no_delete
    BEFORE DELETE ON wallet_ledger_entries
    FOR EACH ROW EXECUTE FUNCTION wallet_ledger_entries_immutable();

-- TRUNCATE nao dispara trigger de linha: precisa de trigger de statement.
CREATE TRIGGER wallet_ledger_entries_no_truncate
    BEFORE TRUNCATE ON wallet_ledger_entries
    FOR EACH STATEMENT EXECUTE FUNCTION wallet_ledger_entries_immutable();