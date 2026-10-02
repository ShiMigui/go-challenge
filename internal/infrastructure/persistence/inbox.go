package persistence

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/shimigui/go-challenge/internal/application/ports"
)

// inboxRepository é a implementação sobre o banco.
type inboxRepository struct {
	db Querier
}

// NewInboxRepository devolve o repositório de inbox.
func NewInboxRepository(db Querier) ports.InboxRepository {
	return &inboxRepository{db: db}
}

// TryBegin registra a mensagem se ainda não existir.
func (r *inboxRepository) TryBegin(ctx context.Context, consumer, messageID, messageHash string, now time.Time) (bool, error) {
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
func (r *inboxRepository) Complete(ctx context.Context, consumer, messageID string, now time.Time) error {
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
func (r *inboxRepository) IsCompleted(ctx context.Context, consumer, messageID string) (bool, error) {
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
func (r *inboxRepository) CountAttempts(ctx context.Context, consumer, messageID string) (int, error) {
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
