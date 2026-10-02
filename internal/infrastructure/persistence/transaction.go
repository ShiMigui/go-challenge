package persistence

import (
	"context"
	"database/sql"

	"github.com/shimigui/go-challenge/internal/application/ports"
	"github.com/shimigui/go-challenge/internal/domain/event"
	"github.com/shimigui/go-challenge/internal/domain/ledger"
	"github.com/shimigui/go-challenge/internal/domain/wager"
	"github.com/shimigui/go-challenge/internal/domain/wallet"
)

// transactionManager implementa ports.TransactionManager sobre o banco.
type transactionManager struct {
	db *sql.DB
}

// NewTransactionManager devolve o port de transação.
func NewTransactionManager(db *sql.DB) ports.TransactionManager {
	return &transactionManager{db: db}
}

// InTransaction roda fn em uma transação, confirmando se ela voltar sem
// erro e desfazendo em caso contrário.
func (m *transactionManager) InTransaction(ctx context.Context, fn func(uow ports.UnitOfWork) error) error {
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(&sqlUnitOfWork{tx: tx}); err != nil {
		if rollbackErr := tx.Rollback(); rollbackErr != nil && rollbackErr != sql.ErrTxDone {
			return rollbackErr
		}
		return err
	}
	return tx.Commit()
}

// sqlUnitOfWork agrupa os repositórios presos à mesma transação.
//
// Cada porta é construída na hora sobre o *sql.Tx: os repositórios são
// baratos de montar e compartilham o mesmo contexto transacional.
type sqlUnitOfWork struct {
	tx *sql.Tx
}

// Wallets devolve o repositório de carteiras da transação.
func (u *sqlUnitOfWork) Wallets() wallet.WalletRepository {
	return NewWalletRepository(u.tx)
}

// Ledger devolve o repositório de lançamentos da transação.
func (u *sqlUnitOfWork) Ledger() ledger.LedgerRepository {
	return NewLedgerRepository(u.tx)
}

// Transactions devolve o repositório de operações da transação.
func (u *sqlUnitOfWork) Transactions() wager.WagerTransactionRepository {
	return NewWagerTransactionRepository(u.tx)
}

// Events devolve o outbox da transação.
func (u *sqlUnitOfWork) Events() event.OutboxRepository {
	return NewOutboxRepository(u.tx)
}

// Messages devolve a deduplicação de mensagens da transação.
func (u *sqlUnitOfWork) Messages() ports.InboxRepository {
	return NewInboxRepository(u.tx)
}
