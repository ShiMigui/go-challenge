package event

import (
	"context"
	"time"
)

// OutboxRepository faz a publicação transacional dos eventos de domínio.
//
// É o port que a aplicação usa para gravar e reivindicar eventos; a
// implementação vive na infraestrutura de persistência.
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
