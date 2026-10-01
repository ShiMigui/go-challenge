package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// Event é um registro do outbox pronto para publicação.
type Event struct {
	id            string
	aggregateType string
	aggregateID   string
	eventType     string
	version       int
	correlationID string
	causationID   string
	payload       []byte
	occurredAt    time.Time
	attempts      int
}

// OutboxRepository faz a publicação transacional dos eventos de domínio.
type OutboxRepository interface {
	// Append grava o evento na mesma transação da mudança que o produziu.
	//
	// É o que garante que o evento exista se e somente se a mudança
	// existir: as duas gravações commitam juntas ou nenhuma.
	Append(ctx context.Context, e *Event) error
	// ClaimBatch reserva eventos pendentes para este worker.
	//
	// O FOR UPDATE SKIP LOCKED deixa vários workers competirem pelo mesmo
	// backlog sem repetir evento: cada linha vai para um worker só.
	ClaimBatch(ctx context.Context, worker string, limite int, now time.Time) ([]*Event, error)
	// MarkPublished carimba a publicação.
	MarkPublished(ctx context.Context, id string, now time.Time) error
	// Reschedule devolve o evento para nova tentativa com backoff.
	Reschedule(ctx context.Context, id string, proximaTentativa time.Time, now time.Time) error
}

// PostgresOutbox é a implementação sobre o Postgres.
type PostgresOutbox struct {
	db Querier
}

// NewOutboxRepository devolve o repositório de outbox.
func NewOutboxRepository(db Querier) *PostgresOutbox {
	return &PostgresOutbox{db: db}
}

// Append grava o evento pendente.
func (r *PostgresOutbox) Append(ctx context.Context, e *Event) error {
	if err := validUUID("event_id", e.id); err != nil {
		return err
	}
	if err := validUUID("aggregate_id", e.aggregateID); err != nil {
		return err
	}
	// A constraint do banco exige objeto JSON, não array nem string.
	if err := validaPayloadObjeto(e.payload); err != nil {
		return fmt.Errorf("evento %s: %w", e.id, err)
	}
	const q = `
		INSERT INTO outbox (
			id, aggregate_type, aggregate_id, event_type, event_version,
			correlation_id, causation_id, payload, occurred_at, next_attempt_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`
	_, err := r.db.ExecContext(ctx, q,
		e.id, e.aggregateType, e.aggregateID, e.eventType, e.version,
		nullString(e.correlationID), nullString(e.causationID),
		e.payload, e.occurredAt, e.occurredAt,
	)
	return err
}

// ClaimBatch reserva uma lote de eventos para o worker.
func (r *PostgresOutbox) ClaimBatch(ctx context.Context, worker string, limite int, now time.Time) ([]*Event, error) {
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
	var saida []*Event
	for _, id := range ids {
		var (
			e             Event
			correlationID sql.NullString
			causationID   sql.NullString
			version       int
		)
		err := r.db.QueryRowContext(ctx, atualiza, worker, now, id).Scan(
			&e.id, &e.aggregateType, &e.aggregateID, &e.eventType, &version,
			&correlationID, &causationID, &e.payload, &e.occurredAt, &e.attempts,
		)
		if err != nil {
			return nil, err
		}
		e.version = version
		e.correlationID = stringFromNull(correlationID)
		e.causationID = stringFromNull(causationID)
		saida = append(saida, &e)
	}
	return saida, nil
}

// lockExpiracao é quanto tempo um lock sobrevive sem publicação.
const lockExpiracao = 60 * time.Second

// MarkPublished carimba a publicação e solta o lock.
func (r *PostgresOutbox) MarkPublished(ctx context.Context, id string, now time.Time) error {
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
func (r *PostgresOutbox) Reschedule(ctx context.Context, id string, proximaTentativa time.Time, now time.Time) error {
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

// NewEvent monta um evento de outbox.
//
// O id é gerado fora e preservado entre republicações: quem consome
// consegue deduplicar pelo mesmo id mesmo vendo o evento mais de uma vez.
func NewEvent(id, aggregateType, aggregateID, eventType string, version int, payload []byte, now time.Time) (*Event, error) {
	if err := validUUID("event_id", id); err != nil {
		return nil, err
	}
	if err := validUUID("aggregate_id", aggregateID); err != nil {
		return nil, err
	}
	if version < 1 {
		return nil, fmt.Errorf("event_version deve ser >= 1, veio %d", version)
	}
	if err := validaPayloadObjeto(payload); err != nil {
		return nil, fmt.Errorf("evento %s: %w", id, err)
	}
	return &Event{
		id:            id,
		aggregateType: aggregateType,
		aggregateID:   aggregateID,
		eventType:     eventType,
		version:       version,
		payload:       payload,
		occurredAt:    now,
	}, nil
}

// ID devolve a identidade estável do evento.
func (e *Event) ID() string { return e.id }

// AggregateType devolve o tipo do agregado de origem.
func (e *Event) AggregateType() string { return e.aggregateType }

// AggregateID devolve a identidade do agregado de origem.
func (e *Event) AggregateID() string { return e.aggregateID }

// EventType devolve o nome do evento.
func (e *Event) EventType() string { return e.eventType }

// Version devolve a versão do contrato.
func (e *Event) Version() int { return e.version }

// CorrelationID devolve a correlação, vazia se não houver.
func (e *Event) CorrelationID() string { return e.correlationID }

// CausationID devolve a causa, vazia se não houver.
func (e *Event) CausationID() string { return e.causationID }

// WithCorrelation devolve uma cópia do evento com correlação e causa.
func (e *Event) WithCorrelation(correlationID, causationID string) *Event {
	copia := *e
	copia.correlationID = correlationID
	copia.causationID = causationID
	return &copia
}

// Payload devolve o snapshot imutável do envelope.
func (e *Event) Payload() []byte { return e.payload }

// OccurredAt devolve o instante em que o fato ocorreu.
func (e *Event) OccurredAt() time.Time { return e.occurredAt }

// Attempts devolve quantas vezes o evento foi tentado.
func (e *Event) Attempts() int { return e.attempts }

// validaPayloadObjeto exige um objeto JSON, que é o que a constraint do
// banco aceita (jsonb_typeof = 'object').
//
// Checar aqui evita a recusa pelo banco, que voltaria como erro genérico
// de constraint em vez de entrada inválida.
func validaPayloadObjeto(payload []byte) error {
	if !json.Valid(payload) {
		return fmt.Errorf("payload nao e JSON valido")
	}
	var objeto map[string]any
	if err := json.Unmarshal(payload, &objeto); err != nil {
		// Unmarshal em map falha para array, string, número e null.
		return fmt.Errorf("payload precisa ser objeto JSON: %w", err)
	}
	if objeto == nil {
		return fmt.Errorf("payload nao pode ser null")
	}
	return nil
}
