// Package ledger modela o livro de lançamentos da carteira.
//
// O ledger é append-only: um lançamento criado nunca é editado ou excluído.
// Correção financeira é um lançamento novo, não uma mudança no antigo.
package ledger

import (
	"errors"
	"fmt"
	"time"

	"github.com/shimigui/go-challenge/internal/domain/money"
)

var (
	// ErrInvalidDirection é devolvido para direção desconhecida.
	ErrInvalidDirection = errors.New("direcao invalida")
	// ErrNonPositiveAmount é devolvido para lançamento com valor não positivo.
	ErrNonPositiveAmount = errors.New("lancamento exige valor positivo")
	// ErrBalanceArithmetic é devolvido quando saldo posterior não bate com a direção.
	ErrBalanceArithmetic = errors.New("saldo posterior inconsistente com a direcao")
	// ErrInvalidWalletID é devolvido sem identidade de carteira.
	ErrInvalidWalletID = errors.New("wallet_id obrigatorio")
	// ErrInvalidTransactionID é devolvido sem identidade de transação.
	ErrInvalidTransactionID = errors.New("transaction_id obrigatorio")
)

// Direction é o sentido do lançamento.
type Direction string

const (
	// Debit reduz o saldo.
	Debit Direction = "DEBIT"
	// Credit aumenta o saldo.
	Credit Direction = "CREDIT"
)

// Valid informa se a direção é conhecida.
func (d Direction) Valid() bool {
	return d == Debit || d == Credit
}

// String devolve o valor do enum.
func (d Direction) String() string { return string(d) }

// Entry é um lançamento imutável no ledger da carteira.
type Entry struct {
	id            string
	walletID      string
	transactionID string
	direction     Direction
	amount        money.Money
	balanceBefore money.Money
	balanceAfter  money.Money
	createdAt     time.Time
}

// Params são os dados de criação de um lançamento.
type Params struct {
	ID            string
	WalletID      string
	TransactionID string
	Direction     Direction
	Amount        money.Money
	BalanceBefore money.Money
	BalanceAfter  money.Money
	Now           time.Time
}

// New cria um lançamento validando a aritmética do saldo.
//
// A regra é balanceAfter = balanceBefore ± amount, conforme a direção. Ela é
// conferida aqui e de novo por CHECK no banco: o código falha rápido, o banco
// garante.
func New(p Params) (*Entry, error) {
	if p.WalletID == "" {
		return nil, ErrInvalidWalletID
	}
	if p.TransactionID == "" {
		return nil, ErrInvalidTransactionID
	}
	if !p.Direction.Valid() {
		return nil, fmt.Errorf("%w: %q", ErrInvalidDirection, string(p.Direction))
	}
	if !p.Amount.IsPositive() {
		return nil, fmt.Errorf("%w: %s", ErrNonPositiveAmount, p.Amount)
	}
	if p.Amount.Currency() != p.BalanceBefore.Currency() ||
		p.Amount.Currency() != p.BalanceAfter.Currency() {
		return nil, fmt.Errorf("%w: %s, %s e %s",
			money.ErrCurrencyMismatch,
			p.Amount.Currency(), p.BalanceBefore.Currency(), p.BalanceAfter.Currency())
	}
	if p.BalanceBefore.IsNegative() {
		return nil, fmt.Errorf("%w: saldo anterior %s", money.ErrNegativeAmount, p.BalanceBefore)
	}
	// O saldo posterior também não pode ser negativo: um débito maior que o
	// saldo produz aritmética coerente e saldo negativo, que a wallet jamais
	// permitiria. A carteira decide isso antes, aqui é a segunda barreira.
	if p.BalanceAfter.IsNegative() {
		return nil, fmt.Errorf("%w: saldo posterior %s", money.ErrNegativeAmount, p.BalanceAfter)
	}
	if err := checkArithmetic(p); err != nil {
		return nil, err
	}
	return &Entry{
		id:            p.ID,
		walletID:      p.WalletID,
		transactionID: p.TransactionID,
		direction:     p.Direction,
		amount:        p.Amount,
		balanceBefore: p.BalanceBefore,
		balanceAfter:  p.BalanceAfter,
		createdAt:     p.Now,
	}, nil
}

// checkArithmetic valida balanceAfter = balanceBefore ± amount.
func checkArithmetic(p Params) error {
	if p.Direction == Credit {
		sum, err := p.BalanceBefore.Add(p.Amount)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrBalanceArithmetic, err)
		}
		if !sum.Equal(p.BalanceAfter) {
			return fmt.Errorf("%w: credito esperava %s, veio %s",
				ErrBalanceArithmetic, sum, p.BalanceAfter)
		}
		return nil
	}
	diff, err := p.BalanceBefore.Sub(p.Amount)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrBalanceArithmetic, err)
	}
	if !diff.Equal(p.BalanceAfter) {
		return fmt.Errorf("%w: debito esperava %s, veio %s",
			ErrBalanceArithmetic, diff, p.BalanceAfter)
	}
	return nil
}

// ForDebit monta o lançamento de um débito que vai ocorrer em w.
//
// Os saldos são lidos da carteira antes da mutação, para que o lançamento
// registre exatamente o par (antes, depois) que a carteira vai assumir.
func ForDebit(id, walletID, transactionID string, amount money.Money, before money.Money, after money.Money, now time.Time) (*Entry, error) {
	return New(Params{
		ID: id, WalletID: walletID, TransactionID: transactionID,
		Direction: Debit, Amount: amount,
		BalanceBefore: before, BalanceAfter: after, Now: now,
	})
}

// ForCredit monta o lançamento de um crédito que vai ocorrer em w.
func ForCredit(id, walletID, transactionID string, amount money.Money, before money.Money, after money.Money, now time.Time) (*Entry, error) {
	return New(Params{
		ID: id, WalletID: walletID, TransactionID: transactionID,
		Direction: Credit, Amount: amount,
		BalanceBefore: before, BalanceAfter: after, Now: now,
	})
}

// ID devolve a identidade do lançamento.
func (e *Entry) ID() string { return e.id }

// WalletID devolve a carteira do lançamento.
func (e *Entry) WalletID() string { return e.walletID }

// TransactionID devolve a transação que originou o lançamento.
func (e *Entry) TransactionID() string { return e.transactionID }

// Direction devolve o sentido do lançamento.
func (e *Entry) Direction() Direction { return e.direction }

// Amount devolve o valor em unidades mínimas, sempre positivo.
func (e *Entry) Amount() money.Money { return e.amount }

// BalanceBefore devolve o saldo anterior.
func (e *Entry) BalanceBefore() money.Money { return e.balanceBefore }

// BalanceAfter devolve o saldo posterior.
func (e *Entry) BalanceAfter() money.Money { return e.balanceAfter }

// CreatedAt devolve o instante do lançamento.
func (e *Entry) CreatedAt() time.Time { return e.createdAt }

// SignedAmount devolve o valor com o sinal da direção.
func (e *Entry) SignedAmount() (money.Money, error) {
	if e.direction == Credit {
		return e.amount, nil
	}
	return e.amount.Neg()
}

// Rehydrate recria um lançamento a partir do banco.
func Rehydrate(id, walletID, transactionID string, direction Direction, amount, balanceBefore, balanceAfter money.Money, createdAt time.Time) *Entry {
	return &Entry{
		id: id, walletID: walletID, transactionID: transactionID,
		direction: direction, amount: amount,
		balanceBefore: balanceBefore, balanceAfter: balanceAfter,
		createdAt: createdAt,
	}
}
