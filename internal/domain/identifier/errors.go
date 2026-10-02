package identifier

import "errors"

// Erros compartilhados de identidade.
//
// Todo agregado valida a presença do próprio id e dos ids referenciados.
// As sentinelas vivem aqui, no pacote do value object, para que wallet,
// wager e ledger usem a mesma identidade de erro em vez de cada um
// manter uma cópia paralela.

var (
	// ErrInvalidID é devolvido quando um identificador não é um UUID válido.
	ErrInvalidID = errors.New("identificador nao e um UUID valido")
	// ErrInvalidPlayerID é devolvido quando o jogador não é identificável.
	ErrInvalidPlayerID = errors.New("player_id obrigatorio")
	// ErrInvalidWalletID é devolvido quando a carteira não tem identidade.
	ErrInvalidWalletID = errors.New("wallet_id obrigatorio")
	// ErrInvalidTransactionID é devolvido sem identidade de transação.
	ErrInvalidTransactionID = errors.New("transaction_id obrigatorio")
)
