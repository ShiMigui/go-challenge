package repository

import (
	"context"
	"database/sql"
)

// TransactionRepository dá acesso ao banco transacional.
//
// Existe separada do Querier porque BeginTx exige *sql.DB: um *sql.Tx
// não abre transação aninhada. Um repositório com Querier aceita os dois,
// mas o ponto de entrada da transação não.
type TransactionRepository interface {
	// WithTx roda fn dentro de uma transação, commitando se ela voltar
	// sem erro e fazendo rollback em caso contrário.
	WithTx(ctx context.Context, fn func(tx *sql.Tx) error) error
}

// PostgresTransaction é a implementação sobre o Postgres.
type PostgresTransaction struct {
	db *sql.DB
}

// NewTransactionRepository devolve o gerenciador de transações.
func NewTransactionRepository(db *sql.DB) *PostgresTransaction {
	return &PostgresTransaction{db: db}
}

// WithTx roda fn em uma transação.
func (r *PostgresTransaction) WithTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		// O erro da função é mais interessante que o do rollback: é
		// ele que explica a falha para quem chamou.
		if rollbackErr := tx.Rollback(); rollbackErr != nil && rollbackErr != sql.ErrTxDone {
			return rollbackErr
		}
		return err
	}
	return tx.Commit()
}
