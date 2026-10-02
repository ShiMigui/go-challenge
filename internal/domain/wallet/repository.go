package wallet

import (
	"context"

	"github.com/shimigui/go-challenge/internal/domain/money"
)

// WalletRepository persiste a carteira (port do agregado).
//
// O agregado declara o que a persistência precisa oferecer; a
// implementação mora na infraestrutura.
type WalletRepository interface {
	// Insert grava uma carteira nova. A unicidade de (player, currency)
	// é do banco; violação volta como ErrDuplicate.
	Insert(ctx context.Context, w *Wallet) error
	// UpdateBalance grava o novo saldo exigindo a versão lida.
	//
	// O version bump fica no trigger, então a escrita envia só balance.
	// A cláusula WHERE version = $3 é o que impede lost update: se
	// outra instância mexeu na carteira no meio, nenhuma linha é
	// atualizada e vem ErrOptimisticLock.
	UpdateBalance(ctx context.Context, w *Wallet) error
	// FindByID devolve a carteira pela identidade.
	FindByID(ctx context.Context, id string) (*Wallet, error)
	// FindByPlayerAndCurrency devolve a carteira de um jogador na moeda.
	FindByPlayerAndCurrency(ctx context.Context, playerID string, currency money.Currency) (*Wallet, error)
	// LockByID devolve a carteira com SELECT FOR UPDATE.
	//
	// Usado quando a operação precisa de saldo estável por mais de uma
	// leitura dentro da mesma transação.
	LockByID(ctx context.Context, id string) (*Wallet, error)
}
