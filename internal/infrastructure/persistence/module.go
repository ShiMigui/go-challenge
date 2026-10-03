// Package persistence implementa a infraestrutura de banco: repositórios
// e transação. É onde mora o código que conhece o SQL.
package persistence

import (
	"context"
	"database/sql"

	"go.uber.org/fx"

	"github.com/shimigui/go-challenge/internal/application/ports"
	"github.com/shimigui/go-challenge/internal/domain/event"
	"github.com/shimigui/go-challenge/internal/domain/ledger"
	"github.com/shimigui/go-challenge/internal/domain/wager"
	"github.com/shimigui/go-challenge/internal/domain/wallet"
)

// Module expõe os ports de persistência para o grafo Fx.
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
		func(db *sql.DB) *MigrationRunner {
			path := "./scripts/postgres/migrations"
			return NewMigrationRunner(db, path)
		},
	),
	fx.Invoke(runMigrationsOnStart),
)

// runMigrationsOnStart roda migrations automaticamente na inicialização.
func runMigrationsOnStart(lc fx.Lifecycle, runner *MigrationRunner) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			return runner.Up(ctx)
		},
	})
}
