package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/shimigui/go-challenge/internal/ledger"
	"github.com/shimigui/go-challenge/internal/money"
)

const ledgerColumns = `id, wallet_id, transaction_id, direction, amount,
	currency, balance_before, balance_after, created_at`

// LedgerRepository persiste os lançamentos da carteira.
type LedgerRepository interface {
	// Append grava um lançamento. A tabela é append-only no banco, então
	// não existe método de update nem de delete por construção.
	Append(ctx context.Context, e *ledger.Entry) error
	// FindByTransaction devolve o lançamento de uma transação.
	//
	// O par (wallet, transaction) é único, então há no máximo um.
	FindByTransaction(ctx context.Context, transactionID string) (*ledger.Entry, error)
	// ListByWallet devolve o extrato da carteira, do mais novo para o mais
	// antigo.
	ListByWallet(ctx context.Context, walletID string, limite int) ([]*ledger.Entry, error)
}

// PostgresLedger é a implementação sobre o Postgres.
type PostgresLedger struct {
	db Querier
}

// NewLedgerRepository devolve o repositório de ledger.
func NewLedgerRepository(db Querier) *PostgresLedger {
	return &PostgresLedger{db: db}
}

// Append grava o lançamento.
func (r *PostgresLedger) Append(ctx context.Context, e *ledger.Entry) error {
	if err := validUUID("entry_id", e.ID()); err != nil {
		return err
	}
	if err := validUUID("wallet_id", e.WalletID()); err != nil {
		return err
	}
	if err := validUUID("transaction_id", e.TransactionID()); err != nil {
		return err
	}
	const q = `
		INSERT INTO wallet_ledger_entries (
			id, wallet_id, transaction_id, direction, amount, currency,
			balance_before, balance_after, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT DO NOTHING
		RETURNING id
	`
	var id string
	err := r.db.QueryRowContext(ctx, q,
		e.ID(), e.WalletID(), e.TransactionID(), string(e.Direction()),
		e.Amount().Amount(), string(e.Amount().Currency()),
		e.BalanceBefore().Amount(), e.BalanceAfter().Amount(), e.CreatedAt(),
	).Scan(&id)
	if err == sql.ErrNoRows {
		// Já existe lançamento para esta transação. Repetir não é erro:
		// reentrega at-least-once tenta de novo e precisa sair quieta.
		return &Duplicate{Err: ErrDuplicate, Operation: "append"}
	}
	if err != nil {
		return err
	}
	return nil
}

// FindByTransaction devolve o lançamento de uma transação.
func (r *PostgresLedger) FindByTransaction(ctx context.Context, transactionID string) (*ledger.Entry, error) {
	if err := validUUID("transaction_id", transactionID); err != nil {
		return nil, err
	}
	q := `SELECT ` + ledgerColumns + ` FROM wallet_ledger_entries WHERE transaction_id = $1`
	row := r.db.QueryRowContext(ctx, q, transactionID)
	e, err := scanLedger(row)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("%w: ledger da transaction %s", ErrNotFound, transactionID)
	}
	if err != nil {
		return nil, err
	}
	return e, nil
}

// ListByWallet devolve o extrato, do mais novo para o mais antigo.
func (r *PostgresLedger) ListByWallet(ctx context.Context, walletID string, limite int) ([]*ledger.Entry, error) {
	if err := validUUID("wallet_id", walletID); err != nil {
		return nil, err
	}
	if limite <= 0 {
		limite = 100
	}
	const q = `
		SELECT ` + ledgerColumns + `
		FROM wallet_ledger_entries
		WHERE wallet_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT $2
	`
	rows, err := r.db.QueryContext(ctx, q, walletID, limite)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var saida []*ledger.Entry
	for rows.Next() {
		e, err := scanLedger(rows)
		if err != nil {
			return nil, err
		}
		saida = append(saida, e)
	}
	return saida, rows.Err()
}

func scanLedger(s scanner) (*ledger.Entry, error) {
	var (
		id, walletID, transactionID string
		direction, currency         string
		amount                      int64
		balanceBefore, balanceAfter int64
		createdAt                   time.Time
	)
	if err := s.Scan(&id, &walletID, &transactionID, &direction, &amount,
		&currency, &balanceBefore, &balanceAfter, &createdAt); err != nil {
		return nil, err
	}
	cur := money.Currency(currency)
	valor, err := money.New(amount, cur)
	if err != nil {
		return nil, fmt.Errorf("ledger %s: %w", id, err)
	}
	antes, err := money.New(balanceBefore, cur)
	if err != nil {
		return nil, fmt.Errorf("ledger %s: %w", id, err)
	}
	depois, err := money.New(balanceAfter, cur)
	if err != nil {
		return nil, fmt.Errorf("ledger %s: %w", id, err)
	}
	return ledger.Rehydrate(id, walletID, transactionID,
		ledger.Direction(direction), valor, antes, depois, createdAt), nil
}
