package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/shimigui/go-challenge/internal/domain/event"
)

// outboxRepository é a implementação sobre o banco.
type outboxRepository struct {
	db Querier
}

// NewOutboxRepository devolve o repositório de outbox.
func NewOutboxRepository(db Querier) event.OutboxRepository {
	return &outboxRepository{db: db}
}

// Append grava o evento pendente.
//
// A forma do payload já foi validada no construtor do evento de domínio;
// aqui resta apenas a barreira técnica do formato de id.
func (r *outboxRepository) Append(ctx context.Context, e *event.Event) error {
	if err := validUUID("event_id", e.ID()); err != nil {
		return err
	}
	if err := validUUID("aggregate_id", e.AggregateID()); err != nil {
		return err
	}
	const q = `
		INSERT INTO outbox (
			id, aggregate_type, aggregate_id, event_type, event_version,
			correlation_id, causation_id, payload, occurred_at, next_attempt_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`
	_, err := r.db.ExecContext(ctx, q,
		e.ID(), e.AggregateType(), e.AggregateID(), e.EventType(), e.Version(),
		nullString(e.CorrelationID()), nullString(e.CausationID()),
		e.Payload(), e.OccurredAt(), e.OccurredAt(),
	)
	return err
}

// ClaimBatch reserva uma lote de eventos para o worker.
func (r *outboxRepository) ClaimBatch(ctx context.Context, worker string, limite int, now time.Time) ([]*event.Event, error) {
	if limite <= 0 {
		limite = 100
	}
	// Duas etapas porque o FOR UPDATE precisa valer só durante o UPDATE:
	// num único statement com subquery, a trava é liberada antes de o
	// worker assumir a linha.
	const seleciona = `
		SELECT id
		FROM outbox
		WHERE published_at IS NULL
			AND next_attempt_at <= $1
			AND (locked_at IS NULL OR locked_at < $2)
		ORDER BY next_attempt_at, occurred_at
		LIMIT $3
		FOR UPDATE SKIP LOCKED
	`
	linhas, err := r.db.QueryContext(ctx, seleciona, now, now.Add(-lockExpiracao), limite)
	if err != nil {
		return nil, err
	}
	var ids []string
	for linhas.Next() {
		var id string
		if err := linhas.Scan(&id); err != nil {
			linhas.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	erro := linhas.Err()
	linhas.Close()
	if erro != nil {
		return nil, erro
	}
	if len(ids) == 0 {
		return nil, nil
	}

	const atualiza = `
		UPDATE outbox
		SET locked_by = $1, locked_at = $2, attempts = attempts + 1
		WHERE id = $3
		RETURNING id, aggregate_type, aggregate_id, event_type, event_version,
			correlation_id, causation_id, payload, occurred_at, attempts
	`
	var saida []*event.Event
	for _, id := range ids {
		var (
			aggregateType string
			aggregateID   string
			eventType     string
			version       int
			correlationID sql.NullString
			causationID   sql.NullString
			payload       []byte
			occurredAt    time.Time
			attempts      int
		)
		err := r.db.QueryRowContext(ctx, atualiza, worker, now, id).Scan(
			&id, &aggregateType, &aggregateID, &eventType, &version,
			&correlationID, &causationID, &payload, &occurredAt, &attempts,
		)
		if err != nil {
			return nil, err
		}
		saida = append(saida, event.Rehydrate(
			id, aggregateType, aggregateID, eventType, version,
			stringFromNull(correlationID), stringFromNull(causationID),
			payload, occurredAt, attempts,
		))
	}
	return saida, nil
}

// lockExpiracao é quanto tempo um lock sobrevive sem publicação.
const lockExpiracao = 60 * time.Second

// MarkPublished carimba a publicação e solta o lock.
func (r *outboxRepository) MarkPublished(ctx context.Context, id string, now time.Time) error {
	if err := validUUID("event_id", id); err != nil {
		return err
	}
	const q = `
		UPDATE outbox
		SET published_at = $2, locked_by = NULL, locked_at = NULL
		WHERE id = $1 AND published_at IS NULL
	`
	res, err := r.db.ExecContext(ctx, q, id, now)
	if err != nil {
		return err
	}
	afetadas, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if afetadas == 0 {
		return fmt.Errorf("%w: evento %s ja publicado ou inexistente", ErrNotFound, id)
	}
	return nil
}

// Reschedule devolve o evento para a fila com novo prazo.
func (r *outboxRepository) Reschedule(ctx context.Context, id string, proximaTentativa time.Time, now time.Time) error {
	if err := validUUID("event_id", id); err != nil {
		return err
	}
	const q = `
		UPDATE outbox
		SET next_attempt_at = $2, locked_by = NULL, locked_at = NULL
		WHERE id = $1 AND published_at IS NULL
	`
	res, err := r.db.ExecContext(ctx, q, id, proximaTentativa)
	if err != nil {
		return err
	}
	afetadas, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if afetadas == 0 {
		return fmt.Errorf("%w: evento %s ja publicado ou inexistente", ErrNotFound, id)
	}
	return nil
}
