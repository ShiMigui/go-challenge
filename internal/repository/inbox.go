package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// InboxRepository faz a deduplicação durável das mensagens consumidas.
type InboxRepository interface {
	// TryBegin registra a mensagem e devolve false se ela já foi
	// confirmada antes.
	//
	// A gravação é durável, não em memória: sobrevive a reinício, então
	// uma reentrega depois de reiniciar o processo continua reconhecida.
	TryBegin(ctx context.Context, consumer, messageID, messageHash string, now time.Time) (bool, error)
	// Complete marca a mensagem como processada.
	//
	// Chamado na mesma transação das mudanças de domínio: enquanto este
	// commit não sair, a mensagem não sai da fila.
	Complete(ctx context.Context, consumer, messageID string, now time.Time) error
	// CountAttempts devolve quantas vezes a mensagem foi begun.
	CountAttempts(ctx context.Context, consumer, messageID string) (int, error)
}

// PostgresInbox é a implementação sobre o Postgres.
type PostgresInbox struct {
	db Querier
}

// NewInboxRepository devolve o repositório de inbox.
func NewInboxRepository(db Querier) *PostgresInbox {
	return &PostgresInbox{db: db}
}

// WithTx returns a new repository using the transaction as querier.
// TryBegin registra a mensagem se ainda não existir.
func (r *PostgresInbox) TryBegin(ctx context.Context, consumer, messageID, messageHash string, now time.Time) (bool, error) {
	// Inserção pura: se a linha volta, a mensagem é inédita e o
	// consumidor deve processá-la.
	const insert = `
		INSERT INTO inbox (consumer_name, message_id, message_hash, received_at, attempts)
		VALUES ($1, $2, $3, $4, 1)
		ON CONFLICT (consumer_name, message_id) DO NOTHING
		RETURNING id
	`
	var id string
	err := r.db.QueryRowContext(ctx, insert, consumer, messageID, messageHash, now).Scan(&id)
	if err == nil {
		return true, nil
	}
	if err != sql.ErrNoRows {
		return false, err
	}
	// A linha já existia. O hash separa reentrega de adulteração: mesmo
	// corpo é reentrega legítima e o consumidor deve sair sem reprocessar;
	// corpo diferente é id reaproveitado, que é erro.
	const consulta = `
		SELECT message_hash
		FROM inbox
		WHERE consumer_name = $1 AND message_id = $2
	`
	var hashGravado string
	if err := r.db.QueryRowContext(ctx, consulta, consumer, messageID).Scan(&hashGravado); err != nil {
		return false, err
	}
	if hashGravado != messageHash {
		return false, fmt.Errorf("%w: %s/%s tem hash %s, chegou %s",
			ErrMessageTampered, consumer, messageID, hashGravado, messageHash)
	}
	return false, nil
}

// Complete marca a mensagem como processada.
func (r *PostgresInbox) Complete(ctx context.Context, consumer, messageID string, now time.Time) error {
	const q = `
		UPDATE inbox
		SET completed_at = $3
		WHERE consumer_name = $1 AND message_id = $2
	`
	res, err := r.db.ExecContext(ctx, q, consumer, messageID, now)
	if err != nil {
		return err
	}
	afetadas, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if afetadas == 0 {
		return fmt.Errorf("%w: %s/%s", ErrNotFound, consumer, messageID)
	}
	return nil
}

// IsCompleted informa se a mensagem já foi processada.
func (r *PostgresInbox) IsCompleted(ctx context.Context, consumer, messageID string) (bool, error) {
	var concluida sql.NullTime
	const q = `
		SELECT completed_at
		FROM inbox
		WHERE consumer_name = $1 AND message_id = $2
	`
	err := r.db.QueryRowContext(ctx, q, consumer, messageID).Scan(&concluida)
	if err == sql.ErrNoRows {
		return false, fmt.Errorf("%w: %s/%s", ErrNotFound, consumer, messageID)
	}
	if err != nil {
		return false, err
	}
	return concluida.Valid, nil
}

// CountAttempts devolve quantas vezes a mensagem foi begun.
func (r *PostgresInbox) CountAttempts(ctx context.Context, consumer, messageID string) (int, error) {
	var tentativas int
	const q = `
		SELECT attempts
		FROM inbox
		WHERE consumer_name = $1 AND message_id = $2
	`
	err := r.db.QueryRowContext(ctx, q, consumer, messageID).Scan(&tentativas)
	if err == sql.ErrNoRows {
		return 0, fmt.Errorf("%w: %s/%s", ErrNotFound, consumer, messageID)
	}
	if err != nil {
		return 0, err
	}
	return tentativas, nil
}
