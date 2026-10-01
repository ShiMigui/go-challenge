package money

import (
	"errors"
	"math"
	"strings"
	"testing"
)

func TestParseValidos(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"0.00", 0},
		{"25.00", 2500},
		{"25.5", 2550},
		{"25.50", 2550},
		{"0.01", 1},
		{"1234567.89", 123456789},
		{"-10.00", -1000},
		{"+10.00", 1000},
		{" 25.00 ", 2500},
	}
	for _, c := range cases {
		got, err := Parse(c.in, BRL)
		if err != nil {
			t.Errorf("Parse(%q) erro inesperado: %v", c.in, err)
			continue
		}
		if got.Amount() != c.want {
			t.Errorf("Parse(%q) = %d, quer %d", c.in, got.Amount(), c.want)
		}
		if got.Currency() != BRL {
			t.Errorf("Parse(%q) moeda = %s", c.in, got.Currency())
		}
	}
}

func TestParseInvalidos(t *testing.T) {
	// Entradas que a spec manda rejeitar: vazio, NaN, Infinity, notação
	// científica, escala excedente e negativos onde não se aceitam.
	cases := []string{
		"",
		"   ",
		"NaN",
		"Infinity",
		"-Infinity",
		"1e5",
		"1E5",
		"25.000",
		"25.0000",
		"25.",
		".50",
		"abc",
		"25,00",
		"2 5.00",
		"25.0a",
		"--25.00",
		"99999999999999999999.00",
	}
	for _, c := range cases {
		if _, err := Parse(c, BRL); err == nil {
			t.Errorf("Parse(%q) deveria ter falhado", c)
		}
	}
}

func TestParseMoedaInvalida(t *testing.T) {
	for _, c := range []Currency{"", "B", "BR", "BRLL", "brl", "B1L", "BRL "} {
		if _, err := Parse("25.00", c); !errors.Is(err, ErrInvalidCurrency) {
			t.Errorf("Parse com moeda %q: esperava ErrInvalidCurrency, veio %v", c, err)
		}
	}
}

func TestString(t *testing.T) {
	cases := []struct {
		amount int64
		want   string
	}{
		{0, "0.00"},
		{1, "0.01"},
		{99, "0.99"},
		{100, "1.00"},
		{2550, "25.50"},
		{10000, "100.00"},
		{-1, "-0.01"},
		{-100, "-1.00"},
		{-2550, "-25.50"},
	}
	for _, c := range cases {
		m, err := New(c.amount, BRL)
		if err != nil {
			t.Fatal(err)
		}
		if got := m.String(); got != c.want {
			t.Errorf("New(%d).String() = %q, quer %q", c.amount, got, c.want)
		}
	}
}

func TestRoundTrip(t *testing.T) {
	for _, v := range []string{"0.00", "0.01", "25.00", "25.55", "999999.99", "-42.42"} {
		m, err := Parse(v, BRL)
		if err != nil {
			t.Fatal(err)
		}
		if got := m.String(); got != v {
			t.Errorf("round trip %q -> %q", v, got)
		}
	}
}

func TestIncompatibilidadeDeMoeda(t *testing.T) {
	brl := MustParse("10.00", BRL)
	usd := MustParse("10.00", USD)

	if _, err := brl.Add(usd); !errors.Is(err, ErrCurrencyMismatch) {
		t.Errorf("Add entre moedas: esperava ErrCurrencyMismatch, veio %v", err)
	}
	if _, err := brl.Sub(usd); !errors.Is(err, ErrCurrencyMismatch) {
		t.Errorf("Sub entre moedas: esperava ErrCurrencyMismatch, veio %v", err)
	}
	if _, err := brl.Cmp(usd); !errors.Is(err, ErrCurrencyMismatch) {
		t.Errorf("Cmp entre moedas: esperava ErrCurrencyMismatch, veio %v", err)
	}
	if _, err := brl.GreaterThan(usd); !errors.Is(err, ErrCurrencyMismatch) {
		t.Errorf("GreaterThan entre moedas: esperava ErrCurrencyMismatch, veio %v", err)
	}
	// Comparar igualdade entre moedas distintas é seguro e deve dar falso.
	if brl.Equal(usd) {
		t.Error("10.00 BRL não pode ser igual a 10.00 USD")
	}
}

func TestAritmetica(t *testing.T) {
	a := MustParse("25.50", BRL)
	b := MustParse("10.25", BRL)

	sum, err := a.Add(b)
	if err != nil {
		t.Fatal(err)
	}
	if got := sum.String(); got != "35.75" {
		t.Errorf("25.50 + 10.25 = %s, quer 35.75", got)
	}

	diff, err := a.Sub(b)
	if err != nil {
		t.Fatal(err)
	}
	if got := diff.String(); got != "15.25" {
		t.Errorf("25.50 - 10.25 = %s, quer 15.25", got)
	}

	neg, err := a.Neg()
	if err != nil {
		t.Fatal(err)
	}
	if got := neg.String(); got != "-25.50" {
		t.Errorf("-(25.50) = %s", got)
	}
	abs, err := neg.Abs()
	if err != nil {
		t.Fatal(err)
	}
	if !abs.Equal(a) {
		t.Error("Abs não devolveu o valor original")
	}
}

func TestSubPodeFicarNegativo(t *testing.T) {
	// Valores negativos são permitidos em cálculo interno, não no saldo.
	small := MustParse("10.00", BRL)
	big := MustParse("25.00", BRL)
	diff, err := small.Sub(big)
	if err != nil {
		t.Fatalf("Sub com resultado negativo deveria ser permitido: %v", err)
	}
	if !diff.IsNegative() {
		t.Error("10.00 - 25.00 deveria ser negativo")
	}
}

func TestOverflow(t *testing.T) {
	max, err := New(math.MaxInt64, BRL)
	if err != nil {
		t.Fatal(err)
	}
	one := MustParse("0.01", BRL)

	if _, err := max.Add(one); !errors.Is(err, ErrInvalidAmount) {
		t.Errorf("soma com overflow deveria falhar, veio %v", err)
	}
	if _, err := max.Add(max); !errors.Is(err, ErrInvalidAmount) {
		t.Errorf("soma max+max deveria falhar, veio %v", err)
	}

	minMoney, err := New(math.MinInt64, BRL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := minMoney.Neg(); !errors.Is(err, ErrInvalidAmount) {
		t.Errorf("negacao de MinInt64 deveria falhar, veio %v", err)
	}
	if _, err := minMoney.Sub(max); !errors.Is(err, ErrInvalidAmount) {
		t.Errorf("subtracao com overflow deveria falhar, veio %v", err)
	}
	if _, err := Parse("99999999999999999999.00", BRL); !errors.Is(err, ErrInvalidAmount) {
		t.Errorf("parse com overflow deveria falhar, veio %v", err)
	}
}

func TestComparacao(t *testing.T) {
	small := MustParse("10.00", BRL)
	big := MustParse("10.01", BRL)

	if c, _ := small.Cmp(big); c != -1 {
		t.Errorf("10.00 cmp 10.01 = %d, quer -1", c)
	}
	if c, _ := big.Cmp(small); c != 1 {
		t.Errorf("10.01 cmp 10.00 = %d, quer 1", c)
	}
	if c, _ := small.Cmp(small); c != 0 {
		t.Errorf("10.00 cmp 10.00 = %d, quer 0", c)
	}
	gt, _ := big.GreaterThan(small)
	if !gt {
		t.Error("10.01 > 10.00 deveria ser true")
	}
}

func TestPredicados(t *testing.T) {
	if !Zero(BRL).IsZero() {
		t.Error("Zero deveria IsZero")
	}
	if Zero(BRL).IsPositive() || Zero(BRL).IsNegative() {
		t.Error("Zero não é positivo nem negativo")
	}
	pos := MustParse("0.01", BRL)
	if !pos.IsPositive() || pos.IsZero() || pos.IsNegative() {
		t.Error("0.01 deveria ser positivo")
	}
	neg := MustParse("-0.01", BRL)
	if !neg.IsNegative() || neg.IsZero() || neg.IsPositive() {
		t.Error("-0.01 deveria ser negativo")
	}
}

func TestFromMajor(t *testing.T) {
	m, err := FromMajor(25, 50, BRL)
	if err != nil {
		t.Fatal(err)
	}
	if got := m.String(); got != "25.50" {
		t.Errorf("FromMajor(25,50) = %s", got)
	}
	// Centavos fora de 0..99 são erro: a escala é fixa em duas casas.
	for _, minor := range []int64{-1, 100, 1000} {
		if _, err := FromMajor(25, minor, BRL); !errors.Is(err, ErrInvalidAmount) {
			t.Errorf("FromMajor(25,%d) deveria falhar", minor)
		}
	}
}

func TestJSON(t *testing.T) {
	m := MustParse("25.00", BRL)
	data, err := m.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	// String, não número: evita perda de precisão em consumidores.
	if string(data) != `"25.00"` {
		t.Errorf("MarshalJSON = %s, quer \"25.00\"", data)
	}

	var back Money
	if err := back.UnmarshalJSON([]byte(`"25.00"`)); err == nil {
		t.Error("UnmarshalJSON sem moeda definida deveria falhar")
	}
	back = Zero(BRL)
	if err := back.UnmarshalJSON([]byte(`"25.00"`)); err != nil {
		t.Fatal(err)
	}
	if !back.Equal(m) {
		t.Errorf("round trip json = %s, quer 25.00", back)
	}
	if err := back.UnmarshalJSON([]byte(`"1e5"`)); err == nil {
		t.Error("json com notacao cientifica deveria falhar")
	}
	if err := back.UnmarshalJSON([]byte(`null`)); err == nil {
		t.Error("json nulo deveria falhar")
	}
}

func TestZeroValueNaoEDinheiro(t *testing.T) {
	// O valor zero não tem moeda, então não é um Money utilizável. Isso
	// impede que um struct não inicializado passe despercebido.
	var m Money
	if m.Currency() != "" {
		t.Error("Money zero deveria ter moeda vazia")
	}
	if m.String() != "0.00" {
		t.Errorf("Money zero.String() = %q", m.String())
	}
}

func TestParseSemResiduoDeFloat(t *testing.T) {
	// O ponto do teste: 0.1 tem que virar exatamente 10 unidades mínimas.
	// Se em algum lugar entrar float, o resíduo aparece aqui.
	cases := []struct {
		in   string
		want int64
	}{
		{"0.1", 10},
		{"0.01", 1},
		{"0.07", 7},
		{"1.1", 110},
		{"12.34", 1234},
		{"1234.56", 123456},
		{"0.29", 29},
		{"0.58", 58},
		{"1.005", 0}, // rejeitado, tratado abaixo
	}
	for _, c := range cases {
		m, err := Parse(c.in, BRL)
		if c.want == 0 {
			if err == nil {
				t.Errorf("Parse(%q) deveria rejeitar escala excedente", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("Parse(%q) erro inesperado: %v", c.in, err)
			continue
		}
		if m.Amount() != c.want {
			t.Errorf("Parse(%q) = %d unidades, quer %d", c.in, m.Amount(), c.want)
		}
	}
	// Nenhuma saída pode conter notação científica ou expoente.
	m, err := Parse("0.07", BRL)
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(m.String(), "eE") {
		t.Errorf("saída com notação científica: %q", m)
	}
}
