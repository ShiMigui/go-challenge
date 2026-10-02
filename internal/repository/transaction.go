package repository

import (
	"context"
	"database/sql"
)

// TransactionManager roda código dentro de uma transação do banco.
//
// Existe separado do Querier porque BeginTx exige *sql.DB: um *sql.Tx
// não abre transação aninhada. Um repositório com Querier aceita os dois,
// mas o ponto de entrada da transação não.
type TransactionManager interface {
	// InTransaction roda fn em uma transação, confirmando se ela voltar
	// sem erro e desfazendo em caso contrário.
	InTransaction(ctx context.Context, fn func(tx *sql.Tx) error) error
}

// transactionManager é a implementação sobre o banco.
type transactionManager struct {
	db *sql.DB
}

// NewTransactionManager devolve o gerenciador de transações.
func NewTransactionManager(db *sql.DB) TransactionManager {
	return &transactionManager{db: db}
}

// InTransaction roda fn em uma transação.
func (r *transactionManager) InTransaction(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		if rollbackErr := tx.Rollback(); rollbackErr != nil && rollbackErr != sql.ErrTxDone {
			return rollbackErr
		}
		return err
	}
	return tx.Commit()
}
