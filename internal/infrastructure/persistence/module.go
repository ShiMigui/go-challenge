// Package persistence implementa a infraestrutura de banco: repositórios
// e transação. É onde mora o código que conhece o SQL.
package persistence

import (
	"database/sql"

	"go.uber.org/fx"

	"github.com/shimigui/go-challenge/internal/application/ports"
	"github.com/shimigui/go-challenge/internal/domain/event"
	"github.com/shimigui/go-challenge/internal/domain/ledger"
	"github.com/shimigui/go-challenge/internal/domain/wager"
	"github.com/shimigui/go-challenge/internal/domain/wallet"
)

// Module expõe os ports de persistência para o grafo Fx.
//
// Os construtores aceitam *sql.DB (que implementa Querier) e ficam
// prontos para as leituras fora de transação; a escrita transacional
// nasce do ports.TransactionManager.
var Module = fx.Module("persistence",
	fx.Provide(
		OpenDB,
		NewTransactionManager,
		func(db *sql.DB) wallet.WalletRepository {
			return NewWalletRepository(db)
		},
		func(db *sql.DB) ledger.LedgerRepository {
			return NewLedgerRepository(db)
		},
		func(db *sql.DB) wager.WagerTransactionRepository {
			return NewWagerTransactionRepository(db)
		},
		func(db *sql.DB) event.OutboxRepository {
			return NewOutboxRepository(db)
		},
		func(db *sql.DB) ports.InboxRepository {
			return NewInboxRepository(db)
		},
	),
)
