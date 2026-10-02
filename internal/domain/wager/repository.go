package wager

import (
	"context"
	"errors"
	"time"
)

// ErrDuplicate é devolvido quando a identidade única já existe.
//
// Vale para (provider, external_transaction_id), (provider,
// idempotency_key) e para a reversão única por referência. O chamador
// trata como reentrega, não como erro.
var ErrDuplicate = errors.New("registro duplicado")

// WagerTransactionRepository persiste as operações de wagering (port do agregado).
type WagerTransactionRepository interface {
	// Insert grava a transação.
	//
	// Identidade repetida volta como ErrDuplicate, com a transação
	// existente em Duplicate. É assim que a idempotência do provedor
	// funciona: a reentrega encontra o registro anterior em vez de
	// processar de novo.
	Insert(ctx context.Context, tx *Transaction) error
	// FindByID devolve a transação pela identidade interna.
	FindByID(ctx context.Context, id string) (*Transaction, error)
	// FindByExternalID devolve a transação pelo par (provider, id externo).
	FindByExternalID(ctx context.Context, providerID, externalID string) (*Transaction, error)
	// FindByIdempotencyKey devolve a transação pela chave de deduplicação.
	FindByIdempotencyKey(ctx context.Context, providerID, key string) (*Transaction, error)
	// UpdateState grava estado, falha, carimbo de conclusão e a espera da
	// referência. Só estado não terminal é aceito.
	UpdateState(ctx context.Context, tx *Transaction) error
	// ResolveReference associa a referência interna já resolvida.
	ResolveReference(ctx context.Context, tx *Transaction) error
	// ListPendingReferences devolve as transações esperando referência cujo
	// prazo já venceu, para o worker reprocessar.
	ListPendingReferences(ctx context.Context, agora time.Time, limite int) ([]*Transaction, error)
}

// Duplicate carrega a transação que já existia quando houve violação de
// unicidade.
type Duplicate struct {
	Err       error
	Existing  *Transaction
	Operation string
}

// Error devolve a descrição do conflito.
func (d *Duplicate) Error() string {
	if d.Existing != nil {
		return d.Err.Error() + ": " + d.Operation + " de " + d.Existing.ID()
	}
	return d.Err.Error() + ": " + d.Operation
}

// Unwrap expõe a causa para errors.Is.
func (d *Duplicate) Unwrap() error { return d.Err }
