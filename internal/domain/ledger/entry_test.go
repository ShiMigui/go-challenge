package ledger

import (
	"errors"
	"testing"
	"time"

	"github.com/shimigui/go-challenge/internal/domain/money"
)

var agora = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func TestNewCredito(t *testing.T) {
	e, err := ForCredit("e1", "w1", "t1",
		money.MustParse("50.00", money.BRL),
		money.MustParse("100.00", money.BRL),
		money.MustParse("150.00", money.BRL), agora)
	if err != nil {
		t.Fatal(err)
	}
	if e.Direction() != Credit {
		t.Errorf("direção = %s", e.Direction())
	}
	if e.Amount().String() != "50.00" {
		t.Errorf("valor = %s", e.Amount())
	}
	if e.BalanceBefore().String() != "100.00" || e.BalanceAfter().String() != "150.00" {
		t.Error("saldos não preservados")
	}
}

func TestNewDebito(t *testing.T) {
	e, err := ForDebit("e1", "w1", "t1",
		money.MustParse("25.50", money.BRL),
		money.MustParse("100.00", money.BRL),
		money.MustParse("74.50", money.BRL), agora)
	if err != nil {
		t.Fatal(err)
	}
	if e.Direction() != Debit {
		t.Errorf("direção = %s", e.Direction())
	}
}

func TestAritmeticaIncoerente(t *testing.T) {
	cases := []struct {
		nome  string
		dir   Direction
		antes string
		valor string
		dep   string
	}{
		{"credito que nao soma", Credit, "100.00", "50.00", "140.00"},
		{"credito que subtrai", Credit, "100.00", "50.00", "50.00"},
		{"debito que nao subtrai", Debit, "100.00", "50.00", "160.00"},
		{"debito que soma", Debit, "100.00", "50.00", "60.00"},
		{"credito de valor errado", Credit, "100.00", "50.00", "125.00"},
	}
	for _, c := range cases {
		_, err := New(Params{
			ID: "e", WalletID: "w", TransactionID: "t",
			Direction:     c.dir,
			Amount:        money.MustParse(c.valor, money.BRL),
			BalanceBefore: money.MustParse(c.antes, money.BRL),
			BalanceAfter:  money.MustParse(c.dep, money.BRL),
			Now:           agora,
		})
		if !errors.Is(err, ErrBalanceArithmetic) {
			t.Errorf("%s: esperava ErrBalanceArithmetic, veio %v", c.nome, err)
		}
	}
}

func TestDebitoEsgotandoSaldo(t *testing.T) {
	// Débito igual ao saldo zera a carteira: válido.
	if _, err := ForDebit("e", "w", "t",
		money.MustParse("100.00", money.BRL),
		money.MustParse("100.00", money.BRL),
		money.Zero(money.BRL), agora); err != nil {
		t.Errorf("débito total deveria ser válido: %v", err)
	}

	// Débito maior que o saldo é rejeitado: o saldo nunca fica negativo.
	_, err := ForDebit("e", "w", "t",
		money.MustParse("100.01", money.BRL),
		money.MustParse("100.00", money.BRL),
		money.MustNew(-1, money.BRL), agora)
	// A aritmética 100.00 - 100.01 = -0.01 é coerente, mas o saldo final
	// negativo é proibido: a wallet teria recusado o débito antes.
	if !errors.Is(err, money.ErrNegativeAmount) {
		t.Errorf("débito além do saldo: esperava ErrNegativeAmount, veio %v", err)
	}
}

func TestValorNaoPositivo(t *testing.T) {
	// Testa valor zero (via Parse) e negativo (via MustNew, pois Parse rejeita sinal).
	zero := money.MustParse("0.00", money.BRL)
	neg := money.MustNew(-100, money.BRL)
	for _, m := range []money.Money{zero, neg} {
		_, err := ForCredit("e", "w", "t",
			m,
			money.MustParse("100.00", money.BRL),
			money.MustParse("100.00", money.BRL), agora)
		if !errors.Is(err, ErrNonPositiveAmount) {
			t.Errorf("valor %s: esperava ErrNonPositiveAmount, veio %v", m.String(), err)
		}
	}
}

func TestValidacoes(t *testing.T) {
	base := func() Params {
		return Params{
			ID: "e1", WalletID: "w1", TransactionID: "t1",
			Direction:     Credit,
			Amount:        money.MustParse("10.00", money.BRL),
			BalanceBefore: money.MustParse("100.00", money.BRL),
			BalanceAfter:  money.MustParse("110.00", money.BRL),
			Now:           agora,
		}
	}

	p := base()
	p.WalletID = ""
	if _, err := New(p); !errors.Is(err, ErrInvalidWalletID) {
		t.Errorf("esperava ErrInvalidWalletID, veio %v", err)
	}

	p = base()
	p.TransactionID = ""
	if _, err := New(p); !errors.Is(err, ErrInvalidTransactionID) {
		t.Errorf("esperava ErrInvalidTransactionID, veio %v", err)
	}

	p = base()
	p.Direction = "OUTROS"
	if _, err := New(p); !errors.Is(err, ErrInvalidDirection) {
		t.Errorf("esperava ErrInvalidDirection, veio %v", err)
	}

	p = base()
	p.Amount = money.MustParse("10.00", money.USD)
	if _, err := New(p); !errors.Is(err, money.ErrCurrencyMismatch) {
		t.Errorf("moeda misturada: esperava ErrCurrencyMismatch, veio %v", err)
	}

	p = base()
	p.BalanceBefore = money.MustNew(-100, money.BRL)
	p.BalanceAfter = money.MustParse("9.00", money.BRL)
	if _, err := New(p); !errors.Is(err, money.ErrNegativeAmount) {
		t.Errorf("saldo anterior negativo: esperava ErrNegativeAmount, veio %v", err)
	}
}

func TestSignedAmount(t *testing.T) {
	credit, err := ForCredit("e", "w", "t", money.MustParse("10.00", money.BRL),
		money.Zero(money.BRL), money.MustParse("10.00", money.BRL), agora)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := credit.SignedAmount()
	if err != nil {
		t.Fatal(err)
	}
	if signed.String() != "10.00" {
		t.Errorf("crédito assinado = %s", signed)
	}

	debit, err := ForDebit("e", "w", "t", money.MustParse("10.00", money.BRL),
		money.MustParse("10.00", money.BRL), money.Zero(money.BRL), agora)
	if err != nil {
		t.Fatal(err)
	}
	signed, err = debit.SignedAmount()
	if err != nil {
		t.Fatal(err)
	}
	if signed.String() != "-10.00" {
		t.Errorf("débito assinado = %s", signed)
	}
}

func TestRehydrate(t *testing.T) {
	e := Rehydrate("e9", "w9", "t9", Debit,
		money.MustParse("5.00", money.BRL),
		money.MustParse("20.00", money.BRL),
		money.MustParse("15.00", money.BRL), agora)
	if e.ID() != "e9" || e.WalletID() != "w9" || e.TransactionID() != "t9" {
		t.Error("identidade não preservada")
	}
	if e.Direction() != Debit || e.Amount().String() != "5.00" {
		t.Error("dados não preservados")
	}
	if !e.CreatedAt().Equal(agora) {
		t.Error("timestamp não preservado")
	}
}

func TestDirectionValid(t *testing.T) {
	if !Debit.Valid() || !Credit.Valid() {
		t.Error("direções válidas não reconhecidas")
	}
	for _, d := range []Direction{"", "DEBIT ", "credit", "CREDITO", "OUT"} {
		if d.Valid() {
			t.Errorf("direção %q deveria ser inválida", d)
		}
	}
}
