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
// Migrations são rodadas em cmd/api/main.go ANTES de subir o Fx app.
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
			// No Fx module não temos a connStr, usamos o construtor padrão
			// A connStr será lida da env var DATABASE_URL no migrateInstance
			return NewMigrationRunner(db, path)
		},
	),
	// runMigrationsOnStart removido: migrations rodam em main.go antes do Fx
)
