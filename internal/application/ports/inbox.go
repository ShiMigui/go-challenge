package ports

import (
	"context"
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
	// IsCompleted informa se a mensagem já foi processada.
	IsCompleted(ctx context.Context, consumer, messageID string) (bool, error)
	// CountAttempts devolve quantas vezes a mensagem foi begun.
	CountAttempts(ctx context.Context, consumer, messageID string) (int, error)
}
