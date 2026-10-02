// Package ports define os contratos que a camada de aplicação usa.
//
// Os ports apontam para fora: repositórios de escrita agrupados em
// transação, deduplicação de mensagens e prontidão de dependências. As
// implementações moram na infraestrutura, e os casos de uso dependem
// apenas destas interfaces.
package ports

import (
	"context"

	"github.com/shimigui/go-challenge/internal/domain/event"
	"github.com/shimigui/go-challenge/internal/domain/ledger"
	"github.com/shimigui/go-challenge/internal/domain/wager"
	"github.com/shimigui/go-challenge/internal/domain/wallet"
)

// TransactionManager roda código dentro de uma transação.
//
// A aplicação não conhece *sql.Tx: a transação é exposta como
// UnitOfWork, com os repositórios presos à mesma unidade de trabalho.
type TransactionManager interface {
	InTransaction(ctx context.Context, fn func(uow UnitOfWork) error) error
}

// UnitOfWork agrupa os ports de escrita de uma transação.
type UnitOfWork interface {
	// Wallets devolve o repositório de carteiras da transação.
	Wallets() wallet.WalletRepository
	// Ledger devolve o repositório de lançamentos da transação.
	Ledger() ledger.LedgerRepository
	// Transactions devolve o repositório de operações da transação.
	Transactions() wager.WagerTransactionRepository
	// Events devolve o outbox da transação.
	Events() event.OutboxRepository
	// Messages devolve a deduplicação de mensagens da transação.
	Messages() InboxRepository
}
