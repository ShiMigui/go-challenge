package wallet

import (
	"errors"
	"testing"
	"time"

	"github.com/shimigui/go-challenge/internal/money"
)

func novaCarteira(t *testing.T, abertura string) *Wallet {
	t.Helper()
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	w, err := New(Params{
		ID:       "wallet-1",
		PlayerID: "player-1",
		Currency: money.BRL,
		Opening:  money.MustParse(abertura, money.BRL),
		Now:      now,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return w
}

func TestNewVersaoInicial(t *testing.T) {
	w := novaCarteira(t, "0.00")
	if w.Version() != 1 {
		t.Errorf("versão inicial = %d, quer 1", w.Version())
	}
	if w.ID() != "wallet-1" || w.PlayerID() != "player-1" {
		t.Error("identidade não preservada")
	}
	if w.Currency() != money.BRL {
		t.Errorf("moeda = %s", w.Currency())
	}
}

func TestNewAceitaSaldoZero(t *testing.T) {
	// A spec aceita zero no saldo inicial.
	if _, err := New(Params{
		ID: "w", PlayerID: "p", Currency: money.BRL,
		Opening: money.Zero(money.BRL), Now: time.Now(),
	}); err != nil {
		t.Errorf("abertura zero deveria ser aceita: %v", err)
	}
}

func TestNewValidacoes(t *testing.T) {
	now := time.Now()
	cases := []struct {
		nome string
		p    Params
		want error
	}{
		{"sem id", Params{PlayerID: "p", Currency: money.BRL, Opening: money.Zero(money.BRL), Now: now}, ErrInvalidWalletID},
		{"sem player", Params{ID: "w", Currency: money.BRL, Opening: money.Zero(money.BRL), Now: now}, ErrInvalidPlayerID},
		{"abertura negativa", Params{ID: "w", PlayerID: "p", Currency: money.BRL, Opening: money.MustParse("-1.00", money.BRL), Now: now}, money.ErrNegativeAmount},
		{"moeda diferente", Params{ID: "w", PlayerID: "p", Currency: money.BRL, Opening: money.MustParse("1.00", money.USD), Now: now}, ErrCurrencyMismatch},
	}
	for _, c := range cases {
		_, err := New(c.p)
		if !errors.Is(err, c.want) {
			t.Errorf("%s: esperava %v, veio %v", c.nome, c.want, err)
		}
	}
}

func TestCreditIncrementaVersao(t *testing.T) {
	w := novaCarteira(t, "0.00")
	now := time.Now()

	if err := w.Credit(money.MustParse("100.00", money.BRL), now); err != nil {
		t.Fatal(err)
	}
	if got := w.Balance().String(); got != "100.00" {
		t.Errorf("saldo = %s, quer 100.00", got)
	}
	if w.Version() != 2 {
		t.Errorf("versão = %d, quer 2", w.Version())
	}

	if err := w.Credit(money.MustParse("50.50", money.BRL), now); err != nil {
		t.Fatal(err)
	}
	if got := w.Balance().String(); got != "150.50" {
		t.Errorf("saldo = %s, quer 150.50", got)
	}
	if w.Version() != 3 {
		t.Errorf("versão = %d, quer 3", w.Version())
	}
}

func TestDebitPreservaSaldoNaoNegativo(t *testing.T) {
	w := novaCarteira(t, "100.00")
	now := time.Now()

	// Débito exato esgota a carteira, mas não deixa negativo.
	if err := w.Debit(money.MustParse("100.00", money.BRL), now); err != nil {
		t.Fatalf("débito exato deveria passar: %v", err)
	}
	if !w.Balance().IsZero() {
		t.Errorf("saldo = %s, quer 0.00", w.Balance())
	}
	if w.Version() != 2 {
		t.Errorf("versão = %d, quer 2", w.Version())
	}
}

func TestDebitSemSaldo(t *testing.T) {
	w := novaCarteira(t, "50.00")
	now := time.Now()

	err := w.Debit(money.MustParse("50.01", money.BRL), now)
	if !errors.Is(err, ErrInsufficientFunds) {
		t.Errorf("esperava ErrInsufficientFunds, veio %v", err)
	}
	// A carteira não pode ter mudado.
	if got := w.Balance().String(); got != "50.00" {
		t.Errorf("saldo alterado após falha: %s", got)
	}
	if w.Version() != 1 {
		t.Errorf("versão alterada após falha: %d", w.Version())
	}
}

func TestOperacoesRejeitamMoedaDiferente(t *testing.T) {
	w := novaCarteira(t, "100.00")
	now := time.Now()
	usd := money.MustParse("10.00", money.USD)

	if err := w.Credit(usd, now); !errors.Is(err, ErrCurrencyMismatch) {
		t.Errorf("crédito em USD: esperava ErrCurrencyMismatch, veio %v", err)
	}
	if err := w.Debit(usd, now); !errors.Is(err, ErrCurrencyMismatch) {
		t.Errorf("débito em USD: esperava ErrCurrencyMismatch, veio %v", err)
	}
	if w.Version() != 1 {
		t.Errorf("moeda errada alterou a versão: %d", w.Version())
	}
}

func TestOperacoesRejeitamValorInvalido(t *testing.T) {
	w := novaCarteira(t, "100.00")
	now := time.Now()

	if err := w.Credit(money.Zero(money.BRL), now); !errors.Is(err, money.ErrInvalidAmount) {
		t.Errorf("crédito zero: esperava ErrInvalidAmount, veio %v", err)
	}
	if err := w.Debit(money.Zero(money.BRL), now); !errors.Is(err, money.ErrInvalidAmount) {
		t.Errorf("débito zero: esperava ErrInvalidAmount, veio %v", err)
	}
	if err := w.Credit(money.MustParse("-5.00", money.BRL), now); !errors.Is(err, money.ErrNegativeAmount) {
		t.Errorf("crédito negativo: esperava ErrNegativeAmount, veio %v", err)
	}
	if err := w.Debit(money.MustParse("-5.00", money.BRL), now); !errors.Is(err, money.ErrNegativeAmount) {
		t.Errorf("débito negativo: esperava ErrNegativeAmount, veio %v", err)
	}
	if w.Version() != 1 {
		t.Errorf("operação inválida alterou a versão: %d", w.Version())
	}
}

func TestSequenciaCreditoDebito(t *testing.T) {
	w := novaCarteira(t, "0.00")
	now := time.Now()

	steps := []struct {
		op     string
		valor  string
		saldo  string
		versao int64
	}{
		{"credito", "100.00", "100.00", 2},
		{"debito", "25.00", "75.00", 3},
		{"credito", "10.00", "85.00", 4},
		{"debito", "85.00", "0.00", 5},
	}
	for i, s := range steps {
		m := money.MustParse(s.valor, money.BRL)
		var err error
		if s.op == "credito" {
			err = w.Credit(m, now)
		} else {
			err = w.Debit(m, now)
		}
		if err != nil {
			t.Fatalf("passo %d (%s %s): %v", i, s.op, s.valor, err)
		}
		if got := w.Balance().String(); got != s.saldo {
			t.Errorf("passo %d: saldo = %s, quer %s", i, got, s.saldo)
		}
		if w.Version() != s.versao {
			t.Errorf("passo %d: versão = %d, quer %d", i, w.Version(), s.versao)
		}
	}
}

func TestCanDebitNaoAlteraEstado(t *testing.T) {
	w := novaCarteira(t, "50.00")

	ok, err := w.CanDebit(money.MustParse("50.00", money.BRL))
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("50.00 <= 50.00 deveria ser possível")
	}
	ok, err = w.CanDebit(money.MustParse("50.01", money.BRL))
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("50.01 > 50.00 não deveria ser possível")
	}
	if w.Version() != 1 || w.Balance().String() != "50.00" {
		t.Error("CanDebit alterou o estado da carteira")
	}
}

func TestRehydrateNaoRevalida(t *testing.T) {
	created := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	updated := time.Date(2026, 9, 15, 11, 30, 0, 0, time.UTC)

	w := Rehydrate("wallet-9", "player-9", money.BRL, 5000, 7, created, updated)
	if w.Version() != 7 {
		t.Errorf("versão = %d, quer 7", w.Version())
	}
	if w.Balance().String() != "50.00" {
		t.Errorf("saldo = %s, quer 50.00", w.Balance())
	}
	if !w.CreatedAt().Equal(created) || !w.UpdatedAt().Equal(updated) {
		t.Error("timestamps não preservados")
	}
}

func TestRehydrateComSaldoZero(t *testing.T) {
	w := Rehydrate("w", "p", money.USD, 0, 1, time.Now(), time.Now())
	if !w.Balance().IsZero() {
		t.Errorf("saldo = %s", w.Balance())
	}
	if w.Currency() != money.USD {
		t.Errorf("moeda = %s", w.Currency())
	}
}

func TestUpdatedAtSoMudaComSaldo(t *testing.T) {
	base := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	w := novaCarteira(t, "10.00")
	antes := w.UpdatedAt()

	depois := base.Add(2 * time.Hour)
	if err := w.Credit(money.MustParse("1.00", money.BRL), depois); err != nil {
		t.Fatal(err)
	}
	if !w.UpdatedAt().Equal(depois) {
		t.Errorf("updatedAt = %v, quer %v", w.UpdatedAt(), depois)
	}
	// Criação não muda o updatedAt.
	if w.CreatedAt().Equal(depois) {
		t.Error("createdAt foi sobrescrito")
	}
	_ = antes
}
