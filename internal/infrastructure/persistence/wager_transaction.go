package persistence

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/shimigui/go-challenge/internal/domain/money"
	"github.com/shimigui/go-challenge/internal/domain/wager"
)

const wagerColumns = `id, kind, provider_id, external_transaction_id,
	idempotency_key, payload_hash, player_id, wallet_id, round_id, game_id,
	currency, amount, reference_external_transaction_id, reference_transaction_id,
	state, failure_code, failure_message, reference_attempts,
	reference_next_attempt_at, observed_balance, created_at, updated_at, processed_at`

// wagerRepository é a implementação sobre o banco.
type wagerRepository struct {
	db Querier
}

// NewWagerTransactionRepository devolve o repositório de transações.
func NewWagerTransactionRepository(db Querier) wager.WagerTransactionRepository {
	return &wagerRepository{db: db}
}

// Insert grava a transação ou devolve a que já existia.
func (r *wagerRepository) Insert(ctx context.Context, tx *wager.Transaction) error {
	if err := validUUID("transaction_id", tx.ID()); err != nil {
		return err
	}
	if err := validUUID("player_id", tx.PlayerID()); err != nil {
		return err
	}
	if err := validUUID("wallet_id", tx.WalletID()); err != nil {
		return err
	}
	const q = `
		INSERT INTO wager_transactions (
			id, kind, provider_id, external_transaction_id, idempotency_key,
			payload_hash, player_id, wallet_id, round_id, game_id, currency,
			amount, reference_external_transaction_id, state
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14
		)
		ON CONFLICT DO NOTHING
		RETURNING id
	`
	var id string
	err := r.db.QueryRowContext(ctx, q,
		tx.ID(), string(tx.Kind()), nullString(tx.ProviderID()),
		nullString(tx.ExternalID()), nullString(tx.IdempotencyKey()),
		nullString(tx.PayloadHash()), tx.PlayerID(), tx.WalletID(),
		nullString(tx.RoundID()), nullString(tx.GameID()),
		string(tx.Currency()), tx.Amount().Amount(),
		nullString(tx.ReferenceExternalID()), string(tx.State()),
	).Scan(&id)
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	// ON CONFLICT DO NOTHING devolve zero linhas: a identidade já existe.
	// A busca seguinte diz qual delas, para o chamador responder com o
	// resultado anterior em vez de erro.
	existente, err := r.findByIdentity(ctx, tx)
	if err != nil {
		return err
	}
	return &wager.Duplicate{Err: wager.ErrDuplicate, Existing: existente, Operation: "insert"}
}

// findByIdentity procura a transação que colidiu com a inserção.
func (r *wagerRepository) findByIdentity(ctx context.Context, tx *wager.Transaction) (*wager.Transaction, error) {
	if tx.ProviderID() != "" && tx.ExternalID() != "" {
		if achada, err := r.FindByExternalID(ctx, tx.ProviderID(), tx.ExternalID()); err == nil {
			return achada, nil
		} else if !errors.Is(err, wager.ErrTransactionNotFound) {
			return nil, err
		}
	}
	if tx.ProviderID() != "" && tx.IdempotencyKey() != "" {
		if achada, err := r.FindByIdempotencyKey(ctx, tx.ProviderID(), tx.IdempotencyKey()); err == nil {
			return achada, nil
		} else if !errors.Is(err, wager.ErrTransactionNotFound) {
			return nil, err
		}
	}
	// OPENING não tem identidade externa: a colisão só pode ser o id.
	if achada, err := r.FindByID(ctx, tx.ID()); err == nil {
		return achada, nil
	} else if !errors.Is(err, wager.ErrTransactionNotFound) {
		return nil, err
	}
	// Nenhuma identidade localizei o conflito. Existing fica vazio, mas o
	// erro ainda é *Duplicate: o chamador decide o que fazer com reentrega
	// e não deve precisar conhecer a causa interna.
	return nil, nil
}

// FindByID devolve a transação pela identidade interna.
func (r *wagerRepository) FindByID(ctx context.Context, id string) (*wager.Transaction, error) {
	if err := validUUID("transaction_id", id); err != nil {
		return nil, err
	}
	q := `SELECT ` + wagerColumns + ` FROM wager_transactions WHERE id = $1`
	return r.scanOne(ctx, q, id)
}

// FindByExternalID devolve a transação pelo par (provider, id externo).
func (r *wagerRepository) FindByExternalID(ctx context.Context, providerID, externalID string) (*wager.Transaction, error) {
	q := `SELECT ` + wagerColumns + `
		FROM wager_transactions
		WHERE provider_id = $1 AND external_transaction_id = $2`
	return r.scanOne(ctx, q, providerID, externalID)
}

// FindByIdempotencyKey devolve a transação pela chave de deduplicação.
func (r *wagerRepository) FindByIdempotencyKey(ctx context.Context, providerID, key string) (*wager.Transaction, error) {
	q := `SELECT ` + wagerColumns + `
		FROM wager_transactions
		WHERE provider_id = $1 AND idempotency_key = $2`
	return r.scanOne(ctx, q, providerID, key)
}

// UpdateState grava a transição de estado.
func (r *wagerRepository) UpdateState(ctx context.Context, tx *wager.Transaction) error {
	if err := validUUID("transaction_id", tx.ID()); err != nil {
		return err
	}
	// A cláusula de estado protege contra transição concorrente: se
	// outra instância já levou a transação a um terminal, zero linhas.
	const q = `
		UPDATE wager_transactions
		SET state = $1,
			failure_code = $2,
			failure_message = $3,
			processed_at = $4,
			reference_next_attempt_at = $5,
			observed_balance = $6,
			updated_at = $7
		WHERE id = $8
			AND state IN ('PENDING', 'PENDING_REFERENCE')
	`
	var processedAt any
	if tx.HasProcessedAt() {
		processedAt = tx.ProcessedAt()
	}
	var nextAttempt any
	if !tx.ReferenceNextAttempt().IsZero() {
		nextAttempt = tx.ReferenceNextAttempt()
	}
	var observed any
	if tx.HasObservedBalance() {
		observed = tx.ObservedBalance().Amount()
	}
	res, err := r.db.ExecContext(ctx, q,
		string(tx.State()), nullString(tx.FailureCode()),
		nullString(tx.FailureMessage()), processedAt, nextAttempt, observed,
		tx.UpdatedAt(), tx.ID(),
	)
	if err != nil {
		// A única violação de unicidade possível aqui é a reversão da
		// mesma referência pelo mesmo tipo já PROCESSED. É conflito de
		// negócio, não falha interna: o provedor tentou reverter duas
		// vezes e a segunda resposta é um Duplicate.
		if isUniqueViolation(err) {
			return fmt.Errorf("%w: reversao duplicada de %s por %s",
				wager.ErrDuplicate, tx.ReferenceID(), tx.Kind())
		}
		return err
	}
	afetadas, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if afetadas == 0 {
		// Zero linhas com id existente significa estado terminal: a
		// transição já aconteceu e não pode ser repetida.
		var existe int
		err := r.db.QueryRowContext(ctx,
			`SELECT 1 FROM wager_transactions WHERE id = $1`, tx.ID()).Scan(&existe)
		switch {
		case err == sql.ErrNoRows:
			return fmt.Errorf("%w: transaction %s", wager.ErrTransactionNotFound, tx.ID())
		case err != nil:
			return err
		default:
			return fmt.Errorf("%w: transaction %s", wager.ErrTerminalTransition, tx.ID())
		}
	}
	return nil
}

// ResolveReference associa a referência interna à transação pendente.
func (r *wagerRepository) ResolveReference(ctx context.Context, tx *wager.Transaction) error {
	if err := validUUID("transaction_id", tx.ID()); err != nil {
		return err
	}
	if err := validUUID("reference_id", tx.ReferenceID()); err != nil {
		return err
	}
	const q = `
		UPDATE wager_transactions
		SET reference_transaction_id = $1, updated_at = $2
		WHERE id = $3 AND state IN ('PENDING', 'PENDING_REFERENCE')
	`
	res, err := r.db.ExecContext(ctx, q, tx.ReferenceID(), tx.UpdatedAt(), tx.ID())
	if err != nil {
		return err
	}
	afetadas, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if afetadas == 0 {
		return fmt.Errorf("%w: transaction %s nao esta em PENDING ou PENDING_REFERENCE",
			wager.ErrInvalidStateTransition, tx.ID())
	}
	return nil
}

// ListPendingReferences devolve as esperas vencidas, mais antigas primeiro.
func (r *wagerRepository) ListPendingReferences(ctx context.Context, agora time.Time, limite int) ([]*wager.Transaction, error) {
	if limite <= 0 {
		limite = 100
	}
	const q = `
		SELECT ` + wagerColumns + `
		FROM wager_transactions
		WHERE state = 'PENDING_REFERENCE'
			AND (reference_next_attempt_at IS NULL OR reference_next_attempt_at <= $1)
		ORDER BY reference_next_attempt_at NULLS FIRST, created_at
		LIMIT $2
	`
	rows, err := r.db.QueryContext(ctx, q, agora, limite)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var saida []*wager.Transaction
	for rows.Next() {
		tx, err := scanWager(rows)
		if err != nil {
			return nil, err
		}
		saida = append(saida, tx)
	}
	return saida, rows.Err()
}

func (r *wagerRepository) scanOne(ctx context.Context, q string, args ...any) (*wager.Transaction, error) {
	row := r.db.QueryRowContext(ctx, q, args...)
	tx, err := scanWager(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: %v", wager.ErrTransactionNotFound, args)
		}
		return nil, err
	}
	return tx, nil
}

// scanner é o que *sql.Row e *sql.Rows têm em comum para Scan.
type scanner interface {
	Scan(dest ...any) error
}

func scanWager(s scanner) (*wager.Transaction, error) {
	var (
		id, kind, currency, state string
		providerID, externalID    sql.NullString
		idempotency, payloadHash  sql.NullString
		playerID, walletID        string
		roundID, gameID           sql.NullString
		amount                    int64
		referenceExtID            sql.NullString
		referenceID               sql.NullString
		failureCode               sql.NullString
		failureMessage            sql.NullString
		referenceAttempts         int
		referenceNext             sql.NullTime
		observedBalance           sql.NullInt64
		createdAt, updatedAt      time.Time
		processedAt               sql.NullTime
	)
	err := s.Scan(
		&id, &kind, &providerID, &externalID, &idempotency, &payloadHash,
		&playerID, &walletID, &roundID, &gameID, &currency, &amount,
		&referenceExtID, &referenceID, &state, &failureCode, &failureMessage,
		&referenceAttempts, &referenceNext, &observedBalance,
		&createdAt, &updatedAt, &processedAt,
	)
	if err != nil {
		return nil, err
	}
	valor, err := money.New(amount, money.Currency(currency))
	if err != nil {
		return nil, fmt.Errorf("transaction %s: %w", id, err)
	}
	observado := money.Money{}
	hasObservado := observedBalance.Valid
	if hasObservado {
		if observado, err = money.New(observedBalance.Int64, money.Currency(currency)); err != nil {
			return nil, fmt.Errorf("transaction %s: saldo observado: %w", id, err)
		}
	}
	return wager.Rehydrate(
		id, wager.Kind(kind), wager.State(state),
		playerID, walletID, valor,
		stringFromNull(roundID), stringFromNull(gameID),
		stringFromNull(providerID), stringFromNull(externalID),
		stringFromNull(idempotency), stringFromNull(payloadHash),
		stringFromNull(referenceExtID), stringFromNull(referenceID),
		stringFromNull(failureCode), stringFromNull(failureMessage),
		referenceAttempts, timeOrZero(referenceNext),
		observado, hasObservado,
		createdAt, updatedAt, timeOrZero(processedAt), processedAt.Valid,
	), nil
}

func timeOrZero(nt sql.NullTime) time.Time {
	if !nt.Valid {
		return time.Time{}
	}
	return nt.Time
}
