-- Saldo observado pela operacao ao concluir o processamento.
--
-- O replay deve devolver o saldo que o provedor observou no processamento
-- original, e nao o saldo atual da carteira. Por isso o resultado e
-- persistido na transacao, na moeda da transacao (coluna currency).
ALTER TABLE wager_transactions
    ADD COLUMN observed_balance BIGINT;

COMMENT ON COLUMN wager_transactions.observed_balance IS
    'Saldo da carteira no instante da conclusao, em unidades minimas da moeda da transacao. NULL enquanto a operacao nao sai de PENDING/PENDING_REFERENCE; o replay devolve este valor.';