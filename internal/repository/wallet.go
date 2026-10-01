package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/shimigui/go-challenge/internal/domain/money"
	"github.com/shimigui/go-challenge/internal/domain/wallet"
)

// walletColumns é a lista de campos usada em leitura, para que scan e
// SELECT não Divirjam.
const walletColumns = `id, player_id, currency, balance, version, created_at, updated_at`

// WalletRepository persiste a carteira.
type WalletRepository interface {
	// Insert grava uma carteira nova. A unicidade de (player, currency)
	// é do banco; violação volta como ErrDuplicate.
	Insert(ctx context.Context, w *wallet.Wallet) error
	// UpdateBalance grava o novo saldo exigindo a versão lida.
	//
	// O version bump fica no trigger, então a escrita envia só balance.
	// A cláusula WHERE version = $3 é o que impede lost update: se
	// outra instância mexeu na carteira no meio, nenhuma linha é
	// atualizada e vem ErrOptimisticLock.
	UpdateBalance(ctx context.Context, w *wallet.Wallet) error
	// FindByID devolve a carteira pela identidade.
	FindByID(ctx context.Context, id string) (*wallet.Wallet, error)
	// FindByPlayerAndCurrency devolve a carteira de um jogador na moeda.
	FindByPlayerAndCurrency(ctx context.Context, playerID string, currency money.Currency) (*wallet.Wallet, error)
	// LockByID devolve a carteira com SELECT FOR UPDATE.
	//
	// Usado quando a operação precisa de saldo estável por mais de uma
	// leitura dentro da mesma transação.
	LockByID(ctx context.Context, id string) (*wallet.Wallet, error)
}

// PostgresWallet é a implementação sobre o Postgres.
type PostgresWallet struct {
	db Querier
}

// NewWalletRepository devolve o repositório de carteira.
func NewWalletRepository(db Querier) *PostgresWallet {
	return &PostgresWallet{db: db}
}

// Insert grava a carteira e carimba o id gerado pelo banco.
func (r *PostgresWallet) Insert(ctx context.Context, w *wallet.Wallet) error {
	if err := validUUID("wallet_id", w.ID()); err != nil {
		return err
	}
	if err := validUUID("player_id", w.PlayerID()); err != nil {
		return err
	}
	const q = `
		INSERT INTO wallets (id, player_id, currency, balance)
		VALUES ($1, $2, $3, $4)
	`
	_, err := r.db.ExecContext(ctx, q,
		w.ID(), w.PlayerID(), string(w.Currency()), w.Balance().Amount())
	return err
}

// UpdateBalance grava o saldo novo se a versão lida ainda for a atual.
func (r *PostgresWallet) UpdateBalance(ctx context.Context, w *wallet.Wallet) error {
	if err := validUUID("wallet_id", w.ID()); err != nil {
		return err
	}
	// Só balance vai no UPDATE: version e updated_at sao do trigger.
	const q = `
		UPDATE wallets
		SET balance = $1
		WHERE id = $2 AND version = $3
	`
	res, err := r.db.ExecContext(ctx, q,
		w.Balance().Amount(), w.ID(), w.Version())
	if err != nil {
		return err
	}
	afetadas, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if afetadas == 0 {
		// Ou a carteira sumiu, ou alguém mexeu nela. A distinção
		// importa: sumiu é erro de referência, mexeu é concorrência.
		var existe int
		err := r.db.QueryRowContext(ctx, `SELECT 1 FROM wallets WHERE id = $1`, w.ID()).Scan(&existe)
		switch {
		case err == sql.ErrNoRows:
			return fmt.Errorf("%w: wallet %s", ErrNotFound, w.ID())
		case err != nil:
			return err
		default:
			return fmt.Errorf("%w: wallet %s", ErrOptimisticLock, w.ID())
		}
	}
	return nil
}

// FindByID devolve a carteira pela identidade.
func (r *PostgresWallet) FindByID(ctx context.Context, id string) (*wallet.Wallet, error) {
	if err := validUUID("wallet_id", id); err != nil {
		return nil, err
	}
	q := `SELECT ` + walletColumns + ` FROM wallets WHERE id = $1`
	return r.scanOne(ctx, q, id)
}

// FindByPlayerAndCurrency devolve a carteira do jogador na moeda.
func (r *PostgresWallet) FindByPlayerAndCurrency(ctx context.Context, playerID string, currency money.Currency) (*wallet.Wallet, error) {
	if err := validUUID("player_id", playerID); err != nil {
		return nil, err
	}
	q := `SELECT ` + walletColumns + ` FROM wallets WHERE player_id = $1 AND currency = $2`
	return r.scanOne(ctx, q, playerID, string(currency))
}

// LockByID devolve a carteira travada para a transação corrente.
func (r *PostgresWallet) LockByID(ctx context.Context, id string) (*wallet.Wallet, error) {
	if err := validUUID("wallet_id", id); err != nil {
		return nil, err
	}
	q := `SELECT ` + walletColumns + ` FROM wallets WHERE id = $1 FOR UPDATE`
	return r.scanOne(ctx, q, id)
}

func (r *PostgresWallet) scanOne(ctx context.Context, q string, args ...any) (*wallet.Wallet, error) {
	var (
		id, playerID, currency string
		balance, version       int64
		createdAt, updatedAt   time.Time
	)
	row := r.db.QueryRowContext(ctx, q, args...)
	if err := row.Scan(&id, &playerID, &currency, &balance, &version, &createdAt, &updatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("%w: wallet %v", ErrNotFound, args)
		}
		return nil, err
	}
	mny, err := money.New(balance, money.Currency(currency))
	if err != nil {
		return nil, fmt.Errorf("wallet %s: moeda invalida %q: %w", id, currency, err)
	}
	return wallet.Rehydrate(id, playerID, mny, version, createdAt, updatedAt), nil
}
