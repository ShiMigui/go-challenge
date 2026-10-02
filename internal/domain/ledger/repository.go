package ledger

import "context"

// LedgerRepository persiste os lançamentos da carteira (port do agregado).
type LedgerRepository interface {
	// Append grava um lançamento. A tabela é append-only no banco, então
	// não existe método de update nem de delete por construção.
	Append(ctx context.Context, e *Entry) error
	// FindByTransaction devolve o lançamento de uma transação.
	//
	// O par (wallet, transaction) é único, então há no máximo um.
	FindByTransaction(ctx context.Context, transactionID string) (*Entry, error)
	// ListByWallet devolve o extrato da carteira, do mais novo para o mais
	// antigo, com paginação.
	ListByWallet(ctx context.Context, walletID string, limite int) ([]*Entry, error)
	// ListByWalletAll devolve todos os lançamentos da carteira, do mais
	// novo para o mais antigo, sem paginação.
	ListByWalletAll(ctx context.Context, walletID string) ([]*Entry, error)
}
