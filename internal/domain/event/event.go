// Package event modela os eventos de domínio publicados via outbox.
//
// O evento nasce na transação da mudança que o produziu e só existe se a
// mudança existir: as duas gravações commitam juntas ou nenhuma.
package event

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/shimigui/go-challenge/internal/domain/identifier"
)

// Event é um fato de domínio pronto para publicação.
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

// NewEvent monta um evento de domínio.
//
// O id é gerado fora e preservado entre republicações: quem consome
// consegue deduplicar pelo mesmo id mesmo vendo o evento mais de uma vez.
func NewEvent(id, aggregateType, aggregateID, eventType string, version int, payload []byte, now time.Time) (*Event, error) {
	if !identifier.IsValid(id) {
		return nil, fmt.Errorf("%w: event_id = %q", identifier.ErrInvalidID, id)
	}
	if !identifier.IsValid(aggregateID) {
		return nil, fmt.Errorf("%w: aggregate_id = %q", identifier.ErrInvalidID, aggregateID)
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

// Rehydrate recria o evento a partir do estado persistido.
//
// Não valida nem recalcula nada: o banco já garantiu as invariantes na
// escrita. O evento sai do ClaimBatch por este caminho.
func Rehydrate(id, aggregateType, aggregateID, eventType string, version int, correlationID, causationID string, payload []byte, occurredAt time.Time, attempts int) *Event {
	return &Event{
		id:            id,
		aggregateType: aggregateType,
		aggregateID:   aggregateID,
		eventType:     eventType,
		version:       version,
		correlationID: correlationID,
		causationID:   causationID,
		payload:       payload,
		occurredAt:    occurredAt,
		attempts:      attempts,
	}
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
