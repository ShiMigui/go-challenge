// Package wallet modela a carteira do jogador.
//
// A carteira é a raiz do agregado financeiro: o saldo só muda por operação
// do próprio agregado, e cada mudança gera o lançamento correspondente no
// ledger, confirmado na mesma transação SQL.
package wallet

import (
	"errors"
	"fmt"
	"time"

	"github.com/shimigui/go-challenge/internal/money"
)

var (
	// ErrInsufficientFunds é devolvido quando o débito excede o saldo.
	ErrInsufficientFunds = errors.New("saldo insuficiente")
	// ErrCurrencyMismatch é devolvido quando a movimentação é de outra moeda.
	ErrCurrencyMismatch = errors.New("moeda da movimentacao difere da carteira")
	// ErrInvalidPlayerID é devolvido quando o jogador não é identificável.
	ErrInvalidPlayerID = errors.New("player_id obrigatorio")
	// ErrInvalidWalletID é devolvido quando a carteira não tem identidade.
	ErrInvalidWalletID = errors.New("wallet_id obrigatorio")
	// ErrClosedTransition é devolvido quando se altera uma carteira inexistente.
	ErrClosedTransition = errors.New("carteira removida nao aceita operacao")
)

// initialVersion é a versão de uma carteira recién-criada.
const initialVersion = 1

// Wallet é o agregado de saldo de um jogador em uma moeda.
//
// O saldo e a versão só são alterados por Credit e Debit. A versão é
// calculado aqui e confirmado no banco por update condicional.
type Wallet struct {
	id        string
	playerID  string
	currency  money.Currency
	balance   money.Money
	version   int64
	createdAt time.Time
	updatedAt time.Time
	active    bool
}

// Params são os dados de criação de uma carteira.
type Params struct {
	ID       string
	PlayerID string
	Currency money.Currency
	Opening  money.Money
	Now      time.Time
}

// New cria uma carteira com saldo inicial.
//
// O valor de abertura pode ser zero: a spec aceita zero no saldo inicial.
func New(p Params) (*Wallet, error) {
	if p.ID == "" {
		return nil, ErrInvalidWalletID
	}
	if p.PlayerID == "" {
		return nil, ErrInvalidPlayerID
	}
	if p.Opening.Currency() != p.Currency {
		return nil, fmt.Errorf("%w: abertura em %s, carteira em %s",
			ErrCurrencyMismatch, p.Opening.Currency(), p.Currency)
	}
	if p.Opening.IsNegative() {
		return nil, fmt.Errorf("%w: abertura %s", money.ErrNegativeAmount, p.Opening)
	}
	return &Wallet{
		id:        p.ID,
		playerID:  p.PlayerID,
		currency:  p.Currency,
		balance:   p.Opening,
		version:   initialVersion,
		createdAt: p.Now,
		updatedAt: p.Now,
		active:    true,
	}, nil
}

// Rehydrate recria a carteira a partir do estado persistido.
//
// Não valida nem recalcula nada: o banco já garantiu as invariantes na
// escrita. Reidratação não pode reexecutar regra de negócio, senão um bug
// de leitura passaria a rejeitar dados legítimos.
func Rehydrate(id, playerID string, currency money.Currency, balanceAmount int64, version int64, createdAt, updatedAt time.Time) *Wallet {
	balance, err := money.New(balanceAmount, currency)
	if err != nil {
		// A moeda veio do próprio banco e já passou pelo CHECK ISO 4217.
		panic(fmt.Sprintf("rehydrate com moeda invalida %q: %v", currency, err))
	}
	return &Wallet{
		id:        id,
		playerID:  playerID,
		currency:  currency,
		balance:   balance,
		version:   version,
		createdAt: createdAt,
		updatedAt: updatedAt,
		active:    true,
	}
}

// ID devolve a identidade da carteira.
func (w *Wallet) ID() string { return w.id }

// PlayerID devolve o jogador dono da carteira.
func (w *Wallet) PlayerID() string { return w.playerID }

// Currency devolve a moeda da carteira.
func (w *Wallet) Currency() money.Currency { return w.currency }

// Balance devolve o saldo atual.
func (w *Wallet) Balance() money.Money { return w.balance }

// Version devolve a versão de concorrência.
func (w *Wallet) Version() int64 { return w.version }

// CreatedAt devolve o instante de criação.
func (w *Wallet) CreatedAt() time.Time { return w.createdAt }

// UpdatedAt devolve o instante da última mudança de saldo.
func (w *Wallet) UpdatedAt() time.Time { return w.updatedAt }

// Credit soma ao saldo e incrementa a versão.
//
// O valor precisa ser positivo: valor zero não é uma operação, e valor
// negativo em crédito viraria um débito silencioso.
func (w *Wallet) Credit(amount money.Money, now time.Time) error {
	if !w.active {
		return ErrClosedTransition
	}
	if amount.Currency() != w.currency {
		return fmt.Errorf("%w: %s vs %s", ErrCurrencyMismatch, amount.Currency(), w.currency)
	}
	if amount.IsNegative() {
		return fmt.Errorf("%w: credito %s", money.ErrNegativeAmount, amount)
	}
	if amount.IsZero() {
		return fmt.Errorf("%w: credito zero", money.ErrInvalidAmount)
	}
	total, err := w.balance.Add(amount)
	if err != nil {
		return fmt.Errorf("credito: %w", err)
	}
	w.balance = total
	w.version++
	w.updatedAt = now
	return nil
}

// Debit subtrai do saldo e incrementa a versão.
//
// O saldo nunca fica negativo: é aqui que a regra é decidida, antes de o
// banco recusar o CHECK.
func (w *Wallet) Debit(amount money.Money, now time.Time) error {
	if !w.active {
		return ErrClosedTransition
	}
	if amount.Currency() != w.currency {
		return fmt.Errorf("%w: %s vs %s", ErrCurrencyMismatch, amount.Currency(), w.currency)
	}
	if amount.IsNegative() {
		return fmt.Errorf("%w: debito %s", money.ErrNegativeAmount, amount)
	}
	if amount.IsZero() {
		return fmt.Errorf("%w: debito zero", money.ErrInvalidAmount)
	}
	enough, err := w.balance.GreaterThanOrEqual(amount)
	if err != nil {
		return fmt.Errorf("debito: %w", err)
	}
	if !enough {
		return fmt.Errorf("%w: saldo %s, debito %s", ErrInsufficientFunds, w.balance, amount)
	}
	total, err := w.balance.Sub(amount)
	if err != nil {
		return fmt.Errorf("debito: %w", err)
	}
	w.balance = total
	w.version++
	w.updatedAt = now
	return nil
}

// CanDebit informa se o débito seria possível, sem alterar a carteira.
func (w *Wallet) CanDebit(amount money.Money) (bool, error) {
	if amount.Currency() != w.currency {
		return false, fmt.Errorf("%w: %s vs %s", ErrCurrencyMismatch, amount.Currency(), w.currency)
	}
	return w.balance.GreaterThanOrEqual(amount)
}
