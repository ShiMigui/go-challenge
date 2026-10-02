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

	"github.com/shimigui/go-challenge/internal/domain/identifier"
	"github.com/shimigui/go-challenge/internal/domain/money"
)

var (
	// ErrInsufficientFunds é devolvido quando o débito excede o saldo.
	ErrInsufficientFunds = errors.New("saldo insuficiente")
	// ErrClosedTransition é devolvido quando se altera uma carteira inexistente.
	ErrClosedTransition = errors.New("carteira removida nao aceita operacao")
	// ErrWalletNotFound é devolvido quando a carteira não existe.
	ErrWalletNotFound = errors.New("carteira nao encontrada")
	// ErrDuplicateWallet é devolvido quando já existe carteira para o par (player, currency).
	ErrDuplicateWallet = errors.New("carteira duplicada para jogador e moeda")
)

// initialVersion é a versão de uma carteira recién-criada.
const initialVersion = 1

// Wallet é o agregado de saldo de um jogador em uma moeda.
//
// O saldo e a versão só são alterados por Credit e Debit. A versão é
// calculado aqui e confirmado no banco por update condicional.
// A moeda é obtida via balance.Currency().
type Wallet struct {
	id        string
	playerID  string
	balance   money.Money // contém a moeda
	version   int64
	createdAt time.Time
	updatedAt time.Time
	active    bool
}

// Params são os dados de criação de uma carteira.
// A moeda vem do Opening.Currency().
type Params struct {
	ID       string
	PlayerID string
	Opening  money.Money
	Now      time.Time
}

// New cria uma carteira com saldo inicial.
//
// O valor de abertura pode ser zero: a spec aceita zero no saldo inicial.
// A moeda da carteira vem do Opening.Currency().
func New(p Params) (*Wallet, error) {
	if p.ID == "" {
		return nil, identifier.ErrInvalidWalletID
	}
	if p.PlayerID == "" {
		return nil, identifier.ErrInvalidPlayerID
	}
	if p.Opening.IsNegative() {
		return nil, fmt.Errorf("%w: abertura %s", money.ErrNegativeAmount, p.Opening)
	}
	return &Wallet{
		id:        p.ID,
		playerID:  p.PlayerID,
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
func Rehydrate(id, playerID string, balance money.Money, version int64, createdAt, updatedAt time.Time) *Wallet {
	return &Wallet{
		id:        id,
		playerID:  playerID,
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

// Currency devolve a moeda da carteira (do balance).
func (w *Wallet) Currency() money.Currency { return w.balance.Currency() }

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
	if err := w.validateMutation(amount, "credito"); err != nil {
		return err
	}
	if amount.IsZero() {
		return fmt.Errorf("%w: credito zero", money.ErrInvalidAmount)
	}
	total, err := w.balance.Add(amount)
	if err != nil {
		return fmt.Errorf("credito: %w", err)
	}
	w.applyMutation(total, now)
	return nil
}

// Debit subtrai do saldo e incrementa a versão.
//
// O saldo nunca fica negativo: é aqui que a regra é decidida, antes de o
// banco recusar o CHECK.
func (w *Wallet) Debit(amount money.Money, now time.Time) error {
	if err := w.validateMutation(amount, "debito"); err != nil {
		return err
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
	w.applyMutation(total, now)
	return nil
}

// CanDebit informa se o débito seria possível, sem alterar a carteira.
func (w *Wallet) CanDebit(amount money.Money) (bool, error) {
	if err := w.validateMutation(amount, "debito"); err != nil {
		return false, err
	}
	if amount.IsZero() {
		return false, fmt.Errorf("%w: debito zero", money.ErrInvalidAmount)
	}
	return w.balance.GreaterThanOrEqual(amount)
}

// validateMutation valida regras comuns a Credit e Debit.
func (w *Wallet) validateMutation(amount money.Money, op string) error {
	if !w.active {
		return ErrClosedTransition
	}
	if amount.Currency() != w.balance.Currency() {
		return fmt.Errorf("%w: %s vs %s", money.ErrCurrencyMismatch, amount.Currency(), w.balance.Currency())
	}
	if amount.IsNegative() {
		return fmt.Errorf("%w: %s %s", money.ErrNegativeAmount, op, amount)
	}
	return nil
}

// applyMutation aplica a mutação comum: atualiza saldo, versão e timestamp.
func (w *Wallet) applyMutation(newBalance money.Money, now time.Time) {
	w.balance = newBalance
	w.version++
	w.updatedAt = now
}
